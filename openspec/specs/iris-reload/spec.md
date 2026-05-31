# iris-reload Specification

## Purpose
TBD - created by archiving change add-daemon-self-management. Update Purpose after archive.
## Requirements
### Requirement: `iris:reload` verb

The plugin SHALL expose `iris:reload` as an MCP tool and CLI subcommand that live-upgrades an iris-managed daemon by pulling the latest default branch, running a project-declared build, and dispatching to a project-declared restart mechanism. Configuration SHALL come from a `.iris.toml` file at the source repo root. Self-vs-cross detection SHALL be automatic.

#### Scenario: Self-reload happy path

- **GIVEN** iris's own source repo has `.iris.toml` with `[build] command = ["make", "build"]` and `[restart] mechanism = "exit_code"`
- **WHEN** `iris:reload` is invoked with no `task_id` and no `path`
- **THEN** iris discovers its source repo via `os.Executable()`, acquires the per-source-repo lock, fetches and fast-forwards origin/main, runs `make build` in the source repo root, writes an audit-log entry, returns a structured result with `mode = "self"` and `restart_pending = true`, releases the lock, and schedules `os.Exit(75)` after the response is flushed

#### Scenario: Cross-reload happy path with launchagent mechanism

- **GIVEN** a managed daemon's source repo has `.iris.toml` with `[restart] mechanism = "launchagent"` and `label = "com.example.daemon"`
- **WHEN** `iris:reload` is invoked with `task_id` pointing at a task whose source repo is that daemon's repo
- **THEN** iris acquires the lock, pulls, builds, runs `launchctl kickstart -k gui/<uid>/com.example.daemon`, captures launchctl output, writes an audit-log entry, returns `mode = "cross"` and `restart_pending = false`, and does NOT exit

### Requirement: Pre-flight refusals before any side effect

Before acquiring the lock or performing any mutation (including the pull), `iris:reload` SHALL run the tree-state pre-flight checks that do not depend on `.iris.toml` content and refuse with a structured error if any fails. `.iris.toml` content refusals (missing file, malformed TOML, schema/mechanism validation) are NOT part of this requirement — they run after the pull, against the post-pull configuration (see "Configuration is loaded and validated after the pull").

To choose the fetch target, `iris:reload` MAY perform a lenient pre-pull read of `.iris.toml` solely to discover a `default_branch` override; this lenient read SHALL NOT produce any refusal — on a missing, malformed, or otherwise unreadable file it yields no override and the default-branch fallback (git `origin/HEAD` → `main`) applies.

#### Scenario: Refuses dirty working tree

- **WHEN** the resolved source repo has uncommitted changes
- **THEN** iris returns a structured error naming the dirty paths and does NOT acquire the lock or pull

#### Scenario: Refuses non-default branch

- **WHEN** the source repo's HEAD is on a branch other than the resolved default branch
- **THEN** iris returns a structured error naming the current branch and the default, and does NOT acquire the lock or pull

#### Scenario: Lenient pre-pull peek resolves the fetch target without refusing

- **WHEN** the pre-pull on-disk `.iris.toml` is missing, malformed, or contains a field the running binary does not recognise
- **THEN** iris does NOT refuse at this step; it resolves `default_branch` from the file's override if the lenient read succeeds, otherwise falls back to git `origin/HEAD` (then `main` with a warning), and proceeds to the pull

### Requirement: Pull behavior

Iris SHALL fetch and fast-forward-only merge origin/<default-branch> before invoking the build step, unless the caller passes `no_pull = true`.

#### Scenario: Default behavior pulls

- **WHEN** `iris:reload` is invoked without `no_pull`
- **THEN** iris runs `git fetch origin <default-branch>` followed by `git merge --ff-only origin/<default-branch>` and records `pulled = true` in the result

#### Scenario: `no_pull` skips the pull

- **WHEN** `iris:reload` is invoked with `no_pull = true`
- **THEN** iris does NOT fetch or merge, records `pulled = false`, and uses the current HEAD for the build

#### Scenario: Refuses divergent history

- **WHEN** local default branch and origin/<default-branch> have diverged (fast-forward is not possible)
- **THEN** iris returns a structured error naming both SHAs and does NOT build

### Requirement: Build step

Iris SHALL run the configured `[build] command` in the configured working directory with the configured environment, capture combined stdout+stderr, and enforce the configured timeout.

#### Scenario: Successful build

- **WHEN** `[build] command = ["make", "build"]` and the build exits 0
- **THEN** iris captures stdout+stderr into `build_output` and proceeds to the restart step

#### Scenario: Build timeout kills the process group

- **WHEN** `[build] timeout_seconds = 10` and the build runs longer
- **THEN** iris kills the build's process group, captures any partial output, and returns a structured timeout error

#### Scenario: Non-zero build exit aborts

- **WHEN** the build exits non-zero
- **THEN** iris returns a structured error containing `build_output` and does NOT proceed to restart

### Requirement: Restart mechanism dispatch

Iris SHALL dispatch to the configured `[restart] mechanism`. v1 supports six mechanisms: `exit_code`, `launchagent`, `launchdaemon`, `signal`, `exec`, `none`. Unsupported values SHALL be refused at parse time.

#### Scenario: exit_code dispatches to self-exit

- **WHEN** `mechanism = "exit_code"` with default `code = 75` on a self-reload
- **THEN** iris responds to the caller, releases the lock, sleeps briefly to ensure the response is flushed, and calls `os.Exit(75)`

#### Scenario: launchagent runs `launchctl kickstart -k gui/<uid>/<label>`

- **WHEN** `mechanism = "launchagent"` with `label = "com.example.x"`
- **THEN** iris runs `launchctl kickstart -k gui/<uid>/com.example.x`, captures stdout+stderr into `restart_output`, and returns success when launchctl exits 0

#### Scenario: launchdaemon warns when iris is not root

- **WHEN** `mechanism = "launchdaemon"` and iris's effective UID is not 0
- **THEN** iris emits a warning in `warnings` naming the elevation issue but still attempts the kickstart

#### Scenario: signal reads pid_file and sends signal

- **WHEN** `mechanism = "signal"` with `pid_file = "/tmp/foo.pid"` and `signal = "SIGTERM"`
- **THEN** iris reads the PID from the file, sends SIGTERM via `syscall.Kill`, and returns the result of the kill call in `restart_output`

#### Scenario: exec runs the configured argv

- **WHEN** `mechanism = "exec"` with `command = ["systemctl", "--user", "restart", "foo"]`
- **THEN** iris runs the argv (no shell), captures stdout+stderr into `restart_output`, and enforces `timeout_seconds` (default 30)

#### Scenario: none is a no-op

- **WHEN** `mechanism = "none"`
- **THEN** iris records `restart_output = ""` and skips any restart action

### Requirement: Self-vs-cross detection

Iris SHALL classify each reload as self or cross based on whether the resolved target source repo equals `os.Executable()`'s canonical git root.

#### Scenario: Self when paths match

- **WHEN** the resolved target source repo equals `os.Executable()`'s canonical git common-dir parent
- **THEN** iris sets `mode = "self"` and `restart_pending = true`, and the exit_code mechanism is the only valid restart

#### Scenario: Cross when paths differ

- **WHEN** the resolved target source repo differs from `os.Executable()`'s canonical git common-dir parent
- **THEN** iris sets `mode = "cross"` and `restart_pending = false`, and exit_code is refused at pre-flight

### Requirement: Per-source-repo lock spans pull, build, and restart

Iris SHALL hold the per-source-repo lock from immediately after pre-flight through the end of the restart step (for cross-reload) or through response flush (for self-reload, releasing before scheduling `os.Exit`).

#### Scenario: Concurrent reloads on the same source repo serialize

- **WHEN** two `iris:reload` calls target the same source repo concurrently
- **THEN** the second call blocks until the first releases the lock

#### Scenario: Lock released before self-reload exit

- **WHEN** a self-reload reaches the exit step
- **THEN** the lock is released before `os.Exit(75)` is scheduled, so the respawned daemon starts with a fresh lock map

### Requirement: Optional pre-flight and verify hooks

Iris SHALL support optional `[pre_flight]` and `[verify]` blocks. `[pre_flight]` runs after the pull and after iris's post-pull configuration validation, but before the build; non-zero exit aborts (no build, no restart). `[verify]` runs after restart for cross-reload only; non-zero exit returns an error but does NOT roll back the restart.

#### Scenario: Pre-flight hook aborts on non-zero exit

- **WHEN** `[pre_flight] command` returns exit code 1
- **THEN** iris returns a structured error containing the captured output, does NOT build or restart, and releases the lock

#### Scenario: Pre-flight hook runs against the freshly-pulled tree

- **WHEN** a reload pulls new commits and `.iris.toml` declares a `[pre_flight]` block
- **THEN** iris runs the post-pull `[pre_flight]` command (reading the hook definition from the post-pull `.iris.toml`) after the fast-forward and before the build

#### Scenario: Verify hook reports failure without rollback

- **WHEN** the restart succeeds but `[verify] command` returns exit code 1
- **THEN** iris returns a structured error containing the verify output; the daemon remains on the new binary

#### Scenario: Verify is skipped for self-reload

- **WHEN** the reload is self-managed
- **THEN** any `[verify]` block in `.iris.toml` is ignored (iris has exited; nothing in-process can verify)

### Requirement: Audit log

Iris SHALL append one JSON line per reload to `~/.iris/reload-history.jsonl`. Each line SHALL include the full structured result, a wall-clock timestamp, and the caller identity.

#### Scenario: Success writes an audit entry

- **WHEN** a reload succeeds
- **THEN** iris appends a JSON line containing `{ timestamp, caller, target_source_repo, mode, pulled, pre_pull_sha, post_pull_sha, build_output, restart_mechanism, restart_output, verify_output, restart_pending, warnings, outcome: "success" }`

#### Scenario: Failure also writes an audit entry

- **WHEN** a reload fails at any step (pre-flight, pull, build, restart, verify)
- **THEN** iris still appends a JSON line with `outcome: "failure"` and the error captured in a `failure_reason` field

#### Scenario: Audit-write failure does not crash the reload

- **WHEN** the audit-log file cannot be opened or written
- **THEN** iris logs the failure to the daemon log but completes the reload normally (best-effort)

### Requirement: `task_id` is optional for `iris:reload`

`iris:reload` SHALL accept either `task_id`, `path`, or neither. When both `task_id` and `path` are supplied, iris SHALL refuse with a structured error.

#### Scenario: No-arg call defaults to self

- **WHEN** `iris:reload` is invoked with neither `task_id` nor `path`
- **THEN** iris resolves the target via `os.Executable()` symlink resolution → walk up to the nearest `.git` directory

#### Scenario: `task_id` resolves like other verbs

- **WHEN** `iris:reload` is invoked with `task_id`
- **THEN** iris uses `verbs.Resolve` to derive the source repo (argus task → worktree → git common-dir)

#### Scenario: `path` resolves via git common-dir

- **WHEN** `iris:reload` is invoked with `path = "/some/path"`
- **THEN** iris runs `git -C /some/path rev-parse --git-common-dir` to canonicalize the source repo

#### Scenario: Both `task_id` and `path` is ambiguous

- **WHEN** `iris:reload` is invoked with both arguments set
- **THEN** iris returns a structured error and resolves nothing

### Requirement: Argus project allowlist enforcement for cross-reload only

Iris SHALL enforce the argus project allowlist on the resolved source repo for cross-reload. Self-reload SHALL NOT consult the allowlist (iris's own repo is implicit).

#### Scenario: Cross-reload to non-allowlisted repo is refused

- **WHEN** the resolved source repo (cross-reload) is not on the argus project allowlist
- **THEN** iris returns a structured error naming the rejected path

#### Scenario: Self-reload skips allowlist

- **WHEN** the reload is classified as self
- **THEN** iris does NOT call argus's `/api/projects/full` for allowlist enforcement

### Requirement: Direct CLI invocation mirrors MCP behavior

The `iris reload [target] [--no-pull]` CLI SHALL call the same `verbs.Reload` Go function as the MCP handler, against the live host shell, with the same structured result — EXCEPT that self-reload from the CLI is refused at pre-flight (see "Refuses CLI self-reload at pre-flight"). CLI cross-reload remains a full mirror of MCP cross-reload.

#### Scenario: CLI with no positional resolves target as self and is then refused

- **WHEN** the user runs `iris reload` from any shell with no positional argument
- **THEN** iris resolves the target via `os.Executable()` (matching the MCP flow), then refuses the CLI self-reload at pre-flight per "Refuses CLI self-reload at pre-flight"

#### Scenario: CLI positional is path when prefixed with `/`, `~`, or `.`

- **WHEN** the user runs `iris reload /some/path` or `iris reload ./repo`
- **THEN** iris treats the argument as a filesystem path; if it resolves to self, the CLI self-reload refusal applies; otherwise the cross-reload flow runs as before

#### Scenario: CLI positional is task_id otherwise

- **WHEN** the user runs `iris reload 17798305...`
- **THEN** iris treats the argument as an argus task_id; if the task's source repo equals self, the CLI self-reload refusal applies; otherwise the cross-reload flow runs as before

### Requirement: Refuses CLI self-reload at pre-flight

When `verbs.Reload` is invoked from the CLI entry point (i.e. `Caller == "cli"`) and the resolved target source repo is iris's own source repo, iris SHALL refuse with a structured error immediately after self-vs-cross detection — before acquiring the per-source-repo lock, before any pre-flight refusals on `.iris.toml` or working tree state, and before any pull or build.

The error message SHALL name the three working alternatives: invoking `iris_reload` via MCP, targeting a different iris-managed project, and manually running `iris run-build` followed by `launchctl kickstart -k gui/$UID/<label>` to bounce the daemon.

The refusal SHALL still write an audit-log entry with `caller = "cli"`, `mode = "self"`, `outcome = "failure"`, and `failure_reason` containing a stable, grep-friendly token (e.g. `cli-self-reload-not-supported`).

This requirement exists because `mechanism = "exit_code"` exits the process that called `verbs.Reload`. From MCP that process is the long-running daemon and the LaunchAgent respawns it from the new binary; from the CLI that process is the short-lived `iris` CLI and the daemon is untouched. Silent success (today's behavior) misleads the operator into thinking the daemon was upgraded when it was not.

#### Scenario: No-arg CLI self-reload is refused

- **GIVEN** the operator runs `iris reload` in a terminal with no positional argument
- **WHEN** the request reaches `verbs.Reload` with `Caller = "cli"` and self-vs-cross detection classifies the target as self
- **THEN** iris returns a structured error containing the token `cli-self-reload-not-supported` and the three redirect options, does NOT acquire the per-source-repo lock, does NOT pull, does NOT build, does NOT dispatch any restart, and writes an audit entry with `outcome = "failure"` and the same reason token

#### Scenario: Explicit self path via CLI is refused

- **GIVEN** the operator runs `iris reload <path>` where `<path>` resolves to iris's own source repo
- **WHEN** the request reaches `verbs.Reload` with `Caller = "cli"` and self-vs-cross detection classifies the target as self
- **THEN** iris returns the same structured error as the no-arg case, with no lock acquired and no side effects, and writes the same audit entry

#### Scenario: task_id resolving to self via CLI is refused

- **GIVEN** the operator runs `iris reload <task_id>` where the argus task's source repo equals iris's own source repo
- **WHEN** the request reaches `verbs.Reload` with `Caller = "cli"` and self-vs-cross detection classifies the target as self
- **THEN** iris returns the same structured error, with no lock acquired and no side effects, and writes the same audit entry

#### Scenario: MCP self-reload is unaffected

- **GIVEN** a Claude session inside an argus sandbox invokes `iris_reload` with no `task_id` and no `path`
- **WHEN** the request reaches `verbs.Reload` with `Caller` set from MCP request context (an argus task_id, the literal `"self"`, or empty — never `"cli"`)
- **THEN** iris runs the full reload flow as before (pre-flight, lock, pull, build, restart dispatch, audit, scheduled exit), and the new refusal does NOT fire

#### Scenario: CLI cross-reload is unaffected

- **GIVEN** the operator runs `iris reload <target>` from a terminal and the target resolves to a source repo that is NOT iris's own
- **WHEN** the request reaches `verbs.Reload` with `Caller = "cli"` and self-vs-cross detection classifies the target as cross
- **THEN** iris runs the full cross-reload flow as before (allowlist check, pre-flight, lock, pull, build, restart dispatch, optional verify hook, audit), and the new refusal does NOT fire

### Requirement: Configuration is loaded and validated after the pull

`iris:reload` SHALL load and validate `.iris.toml` AFTER the fetch + fast-forward-merge (or, with `no_pull = true`, after the no-op pull step), against the post-pull on-disk file — the configuration the rebuilt-and-restarted binary will actually consume. The structured failure shape (`valid: false` with an `errors` array of `{ field, message, hint }`) is preserved; it simply surfaces at this later step. Every `.iris.toml` content refusal — missing file, malformed TOML, unsupported `schema_version`, cross-mechanism field mismatch, `exit_code` legality — SHALL fire here, not before the pull.

#### Scenario: Validation runs against the post-pull config

- **WHEN** a reload pulls new commits that change `.iris.toml`
- **THEN** iris validates the post-pull `.iris.toml` (not the pre-pull file) and uses the post-pull `[build]` and `[restart]` blocks for the build and restart

#### Scenario: Refuses missing `.iris.toml` after the pull

- **WHEN** the source repo root does not contain a readable `.iris.toml` after the pull
- **THEN** iris returns a structured error naming the expected path, and does so after the pull rather than before it

#### Scenario: Refuses malformed TOML after the pull

- **WHEN** the post-pull `.iris.toml` is not syntactically valid TOML
- **THEN** iris returns a structured error naming the parse failure (with a line number when available) and does NOT build or restart

#### Scenario: Refuses unknown schema_version after the pull

- **WHEN** the post-pull `.iris.toml` is missing `schema_version` or sets it to a value iris does not support
- **THEN** iris returns a structured error naming the field, the offending value, and the supported versions — even when unknown-field tolerance is in effect

#### Scenario: Refuses cross-mechanism field mismatch after the pull

- **WHEN** the post-pull `.iris.toml` declares `[restart] mechanism = "launchagent"` but also sets a `pid_file` field (which only belongs to the `signal` mechanism)
- **THEN** iris returns a structured error naming both the declared mechanism and the conflicting field

#### Scenario: Refuses exit_code mechanism for cross-reload after the pull

- **WHEN** the post-pull `.iris.toml` declares `[restart] mechanism = "exit_code"` but the resolved source repo is NOT iris's own deployed source repo
- **THEN** iris returns a structured error stating that `exit_code` is a self-only mechanism

### Requirement: Forward-compatible unknown fields are tolerated during reload pre-flight

During reload pre-flight, `iris:reload` SHALL decode `.iris.toml` in a forward-compatible mode in which unknown fields (top-level or nested) are downgraded from validation errors to non-fatal warnings. Each tolerated unknown field SHALL be surfaced as a warning in the structured result's `warnings` and in the audit-log entry. This makes an additive `.iris.toml` schema change deployable in a single reload: the freshly-pulled new field is tolerated by the old binary's decoder, the build produces the new binary, and the restart brings up a binary that fully understands the field. This tolerance applies ONLY to reload (and publish) pre-flight; `iris:validate_config` SHALL remain strict.

#### Scenario: Unknown field is tolerated and the reload proceeds

- **WHEN** the post-pull `.iris.toml` contains a field the running binary does not recognise, but is otherwise valid
- **THEN** iris does NOT refuse; it records a warning naming the unknown field, proceeds to build and restart, and includes the warning in the result and audit entry

#### Scenario: schema_version mismatch is not tolerated

- **WHEN** the post-pull `.iris.toml` sets `schema_version` to an unsupported value (in addition to any unknown fields)
- **THEN** iris still refuses with a structured `schema_version` error — version mismatch is a hard refusal even though unknown fields are tolerated

#### Scenario: Malformed TOML is not tolerated

- **WHEN** the post-pull `.iris.toml` fails to parse as TOML
- **THEN** iris still refuses with a structured parse error — forward-compat tolerance applies to unknown fields, not to syntax errors

