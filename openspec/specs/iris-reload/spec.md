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

Before acquiring the lock or performing any mutation, `iris:reload` SHALL run pre-flight checks and refuse with a structured error if any fails.

#### Scenario: Refuses dirty working tree

- **WHEN** the resolved source repo has uncommitted changes
- **THEN** iris returns a structured error naming the dirty paths and does NOT acquire the lock

#### Scenario: Refuses non-default branch

- **WHEN** the source repo's HEAD is on a branch other than the resolved default branch
- **THEN** iris returns a structured error naming the current branch and the default, and does NOT acquire the lock

#### Scenario: Refuses missing `.iris.toml`

- **WHEN** the source repo root does not contain a readable `.iris.toml` file
- **THEN** iris returns a structured error naming the expected path

#### Scenario: Refuses unknown schema_version

- **WHEN** `.iris.toml` is missing the `schema_version` field or sets it to a value iris does not support
- **THEN** iris returns a structured error naming the field, the offending value, and the supported versions

#### Scenario: Refuses cross-mechanism field mismatch

- **WHEN** `.iris.toml` declares `[restart] mechanism = "launchagent"` but also sets a `pid_file` field (which only belongs to the `signal` mechanism)
- **THEN** iris returns a structured error naming both the declared mechanism and the conflicting field

#### Scenario: Refuses exit_code mechanism for cross-reload

- **WHEN** `.iris.toml` declares `[restart] mechanism = "exit_code"` but the resolved source repo is NOT iris's own deployed source repo
- **THEN** iris returns a structured error stating that `exit_code` is a self-only mechanism

#### Scenario: Refuses non-exit_code mechanism for self-reload

- **WHEN** `.iris.toml` declares `[restart] mechanism` other than `exit_code` (e.g. `launchagent`, `signal`, `exec`, `none`) AND the resolved source repo IS iris's own deployed source repo
- **THEN** iris returns a structured error stating that self-managed daemons must use `exit_code` (the response-flush + lock-release + delayed-exit choreography is only correct for exit_code)

#### Scenario: Refuses zero exit_code

- **WHEN** `.iris.toml` declares `[restart] mechanism = "exit_code"` with `code = 0`
- **THEN** iris returns a structured error stating that the code must be non-zero

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

Iris SHALL support optional `[pre_flight]` and `[verify]` blocks. `[pre_flight]` runs after iris's built-in pre-flight refusals but before pull; non-zero exit aborts. `[verify]` runs after restart for cross-reload only; non-zero exit returns an error but does NOT roll back the restart.

#### Scenario: Pre-flight hook aborts on non-zero exit

- **WHEN** `[pre_flight] command` returns exit code 1
- **THEN** iris returns a structured error containing the captured output, does NOT pull or build, and releases the lock

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

The `iris reload [target] [--no-pull]` CLI SHALL call the same `verbs.Reload` Go function as the MCP handler, against the live host shell, with the same structured result.

#### Scenario: CLI with no positional defaults to self

- **WHEN** the user runs `iris reload` from any shell with no positional argument
- **THEN** iris resolves the target via `os.Executable()` and runs the same flow as the MCP handler

#### Scenario: CLI positional is path when prefixed with `/`, `~`, or `.`

- **WHEN** the user runs `iris reload /some/path` or `iris reload ./repo`
- **THEN** iris treats the argument as a filesystem path

#### Scenario: CLI positional is task_id otherwise

- **WHEN** the user runs `iris reload 17798305...`
- **THEN** iris treats the argument as an argus task_id

