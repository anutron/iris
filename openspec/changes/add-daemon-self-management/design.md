# Design: daemon self-management subsystem

Scope: four verbs (`iris:reload`, `iris:validate_config`, `iris:ls`, `iris:status`) plus the `.iris.toml` declarative convention plus a structured audit log. Reload owns the bulk of the design surface; the other three are read or parse verbs that ride on the convention and audit log this change introduces.

## Context

Iris v1.0 ships six verbs that operate on agent-supplied argus tasks. None of them upgrade a running daemon. The only path today is re-running `setup.sh` (or the equivalent install script), which does too much — re-mints tokens, rewrites the LaunchAgent plist, runs the whole install on every roll. The intended workflow ("merge a code-only PR, then live-swap the binary") has no first-class verb.

This change introduces `iris:reload` and a small declarative file (`.iris.toml`) that lets iris reload itself or any other daemon it is asked to manage. Iris itself becomes the first "managed system" but the design is built so any future LaunchAgent-style daemon adopts the same shape without iris changes.

### Why declarative instead of a script

Earlier drafts of this design proposed an executable `script/iris-reload` as the convention. That was rejected after the /doitright analysis. The reasoning:

- The audience for the convention is not a human typing it once. It is either a project author writing to iris's published spec, or Claude generating the file on the project's behalf. Familiarity with shell scripting is not a real constraint.
- Iris is a systems-management tool. Every mature systems-management ecosystem (systemd, Kubernetes, Docker Compose, launchd, nix) chose declarative configuration over scripts for the same reasons: introspection, validation, predictable evolution, structured failure reporting.
- A script gives the project author flexibility iris cannot see into. Iris can enforce only the rails *around* the script (lock, clean tree, ff-only pull). Anything inside the script is opaque to iris's audit log, status surface, and future tooling.
- Declarative with an `exec` escape hatch gives equivalent flexibility for genuinely unusual restart mechanisms without surrendering the structured shape for everything else.

### Constraints inherited from v1.0

- **LaunchAgent `KeepAlive: SuccessfulExit=false`** (from `iris-host-bridge` "Graceful shutdown stays down" and observed in `setup.sh`). Clean SIGTERM → exit 0 → no respawn. Self-reload's respawn must use a non-zero exit, which is a behavior `launchctl bootout` already tolerates (bootout removes the agent registration entirely).
- **`task_id`-everywhere invariant** (from `iris-host-bridge` "Source-repo path resolution from `task_id`"). Reload is the first verb where the caller has no argus task to point at when reloading iris itself. The spec needs a small carve-out for self-hosting verbs.
- **Per-source-repo lock** (existing `lockSourceRepo` primitive in `internal/verbs/locks.go`). Reload must hold it across pull + build + restart so no other verb on the same source repo interleaves.
- **MCP response must complete before iris exits.** If iris exits mid-handler, the caller sees a dropped connection rather than a structured result. Self-reload must flush the response, *then* schedule the exit.

### Stakeholders

- The operator (Aaron) — initiates reloads, owns post-incident recovery.
- The project author for any managed daemon — writes the `.iris.toml`. Often Claude on the author's behalf.
- Future iris-managed daemons — the convention must work for them without iris changes.

## Goals

- Live-upgrade iris (and any other iris-managed daemon) without re-running install scripts.
- A single declarative convention (`.iris.toml`) that captures everything iris needs to reload a managed system.
- Strict safety rails owned by iris around every reload: lock, clean working tree, default-branch check, fast-forward-only `git pull`. No way to disable them at the project author's discretion.
- Self-reload (iris reloading itself) and cross-reload (iris reloading another daemon) use the same caller-facing contract. The daemon detects self vs cross internally.
- Schema versioning baked in from day one so iris can evolve the file format without breaking deployed configs.
- A small but complete set of restart mechanisms that covers the realistic v1.x landscape, with an `exec` escape hatch for the long tail.
- Structured result on every reload (success and failure) so the caller, the audit log, and future status tooling all see the same data.

## Non-goals

- No CI status pre-check before reload. The operator is responsible for merging a green PR. Matches the v1 stance on `gh_pr_merge`.
- No `make test` gate before restart. Same rationale.
- No "rollback" verb. If reload puts the daemon into a bad state, the operator re-runs the install script or reverts the PR and reloads again.
- No managed-system registry, scanning, or auto-discovery. Iris does not maintain its own inventory of what it manages. Each reload call names its target (path or task_id) explicitly, or implicitly targets self.
- No multi-target `.iris.toml` (no `[[targets]]` array). v1.1 assumes one managed daemon per source repo. The schema is shaped so `[[targets]]` could be added in schema_version 2 without breaking v1 files (see "Forward compatibility").
- No project author writing OR reading TOML syntax by hand. The expectation is iris will publish a documented schema; humans and agents author against it.
- No `iris-validate-config`, `iris-ls`, or per-system `iris-status` verbs in v1.1. The design preserves the option of adding them later (see "Forward compatibility").

## Decisions

### D1. The convention is a declarative TOML file at the repo root

`<source-repo-root>/.iris.toml` defines everything iris needs to reload the daemon: schema version, default branch (optional override of git's `origin/HEAD`), build command, restart mechanism, and optional pre-flight and verify hooks.

**Rationale.** A declarative file is introspectable, validatable, and structured. Iris can refuse malformed files at parse time, surface what it intends to do before doing it (a future `iris reload --dry-run`), and produce uniform audit events across every managed system. Project authors write to a published schema once; iris does the rest.

**Alternative considered (rejected):** Executable `script/iris-reload` at the repo root. Rejected because it gives the project author flexibility iris cannot observe, validate, or evolve uniformly. The "exec" escape hatch in the declarative schema recovers the flexibility for the long tail of unusual restart needs.

**Alternative considered (rejected):** YAML (`.iris.yaml`). YAML is more capable but whitespace-sensitive, error-prone, and adds more parser surface area than needed. TOML is mature, simple, typed.

**Alternative considered (rejected):** JSON (`.iris.json`). JSON has no comments and is noisier to write by hand or generate. TOML's commentable, line-oriented form fits configuration better.

**Library choice:** `github.com/BurntSushi/toml`. Most-used Go TOML parser, mature, used by Hugo and many other production tools.

### D2. Schema versioning is required from day one

Every `.iris.toml` MUST include `schema_version = 1`. Iris refuses files with a missing version or with a version it does not understand. When iris ships schema_version 2, it accepts version 1 files with a deprecation warning surfaced in the result.

**Rationale.** Without versioning, evolving the schema is a coordination crisis. Versioning costs nothing now and unlocks safe evolution forever. This is industry standard (Docker Compose's `version: "3.x"`, Kubernetes' `apiVersion`, every well-designed config format).

### D3. Self-vs-cross detection is automatic

Iris compares the resolved target source repo against `os.Executable()`'s symlink-resolved git root. If they match, this is a self-reload — iris will respond to the caller and then exit with the configured code so the LaunchAgent respawns from the new binary. If they differ, this is a cross-reload — iris responds to the caller and trusts the configured restart mechanism to bring the target back up.

**Rationale.** The caller (and the project author) should not need to know whether they are reloading iris itself or another daemon. Detection is unambiguous from filesystem state and cannot be spoofed by an argus-side caller (the comparison is on canonicalized paths iris derives itself).

**Alternative considered (rejected):** Explicit `--self` flag or `target = "self"` field in `.iris.toml`. Rejected because it adds a footgun: if the operator (or Claude) forgets the flag, iris restarts something else and stays on the old binary.

### D4. Self-reload uses the `exit_code` restart mechanism

For self-managed daemons (iris itself), the configured restart mechanism MUST be `exit_code`. The mechanism's `code` field (default 75) is the non-zero exit code iris uses when it exits, so the LaunchAgent's `KeepAlive: SuccessfulExit=false` respawns the process from the new binary.

**Why exit_code is the only valid mechanism for self-reload.** Any other mechanism would require iris to either (a) run a command against itself (e.g., `launchctl kickstart -k com.anutron.iris`) which is fragile when the calling process is the one being killed, or (b) signal itself via PID, which is the same problem at the system-call level. Exiting non-zero and letting the supervisor respawn is the canonical Unix pattern.

**Why 75 is the default.** 75 is `EX_TEMPFAIL` from `sysexits.h`. Iris's own non-reload codepaths use 0 (clean shutdown) and 1 (unrecoverable startup error). A reload exit is conceptually "I'm leaving so the supervisor can bring me back" — a temporary failure from the supervisor's perspective. Using a named sysexits code keeps the meaning clear in logs and grep-ability.

**Risk acknowledged.** A genuine iris bug that calls `os.Exit(75)` would also trigger a respawn. This is not new behavior — today `KeepAlive: SuccessfulExit=false` already respawns on any non-zero exit. Reload does not introduce a new failure mode; it relies on the existing one.

### D5. Cross-reload restart is dispatched by mechanism

For cross-managed daemons, iris runs the configured restart command and captures its output. Five mechanisms are supported in v1.1:

- **`exit_code`** — only valid when iris IS the target. Refused for cross-reload.
- **`launchagent`** — runs `launchctl kickstart -k gui/<uid>/<label>` against the configured label. For macOS LaunchAgents (the common case).
- **`launchdaemon`** — runs `launchctl kickstart -k system/<label>`. For system-wide LaunchDaemons. Note: most LaunchDaemons require root; iris does not run as root by default. Document the caveat; emit a warning if iris detects it is not root when this mechanism is selected.
- **`signal`** — reads PID from the configured `pid_file` and sends the configured `signal` (e.g., SIGTERM, SIGHUP, SIGUSR2). For supervisord, foreman, Mina-style Ruby apps, and any process that respawns under a parent supervisor.
- **`exec`** — runs the configured `command` (argv list). Escape hatch for anything not in the above list — `systemctl --user restart foo`, `kill -HUP $(cat /var/run/foo.pid)`, custom orchestration scripts.
- **`none`** — does nothing for restart. For cases where build IS the deployment (static-site generators, asset pipelines that publish on build).

**Why these five.** Covers the realistic landscape of macOS + Unix daemon supervisors a project at Aaron's scale would encounter, plus an escape hatch for everything else. Adding more is non-breaking; removing one is a schema version bump.

**Why no `systemd`-named mechanism.** macOS doesn't have systemd; iris doesn't have a Linux deploy target today. `exec` with `systemctl` works fine if the day comes.

### D6. Restart mechanism fields are exclusive per mechanism

The `[restart]` block has `mechanism = "..."` plus mechanism-specific required fields:

- `exit_code`: optional `code = <int>` (default 75; must be non-zero)
- `launchagent` / `launchdaemon`: required `label = "..."`
- `signal`: required `pid_file = "..."` and `signal = "SIG..."`
- `exec`: required `command = [...]` (argv)
- `none`: no fields

Iris refuses a file at parse time that has fields for a different mechanism than the declared one (e.g., `mechanism = "launchagent"` with a `pid_file` field). The error names the conflict.

**Rationale.** Cross-validation at parse time catches typos and misconfigurations before iris attempts a live reload. Strict-mode TOML parsing rejects unknown fields by default; iris should opt into that posture.

### D7. Iris always pulls; opt-out via `no_pull`

By default, iris does `git fetch origin <default-branch>` + `git merge --ff-only origin/<default-branch>` before invoking the build. The caller may pass `no_pull = true` (MCP) or `--no-pull` (CLI) to rebuild from current HEAD without fetching.

**Rationale.** The user's stated goal is "live-upgrade *after a merged PR*." Pulling is part of the workflow; making the build command responsible for pulling means every project author writes the same three lines.

**Refusal cases (all pre-pull):**

- Working tree dirty → structured error names the dirty paths.
- Not on default branch → structured error names current branch and the declared default.
- Source repo is not a git repository → structured error.
- Origin remote missing or unreachable → structured error.

**Refusal case (during pull):**

- Fast-forward not possible (divergent history) → structured error names the local and remote SHAs.

### D8. Default branch resolution

The default branch is resolved in this priority order:

1. `default_branch` field in `.iris.toml` (explicit override).
2. `git symbolic-ref refs/remotes/origin/HEAD` (the standard mechanism).
3. Fall back to `main`, with a structured warning in the result.

**Rationale.** Step 2 is the canonical mechanism. Step 1 covers repos where origin/HEAD is unset or wrong (common after a default-branch rename). Step 3 keeps reload working in edge cases but tells the operator about it.

### D9. Lock scope spans pre-flight + pull + build + restart

The per-source-repo lock (`lockSourceRepo`) is acquired immediately after pre-flight refusal checks and held until the restart mechanism returns (for cross-reload) or the response is flushed (for self-reload). The lock is released BEFORE the `os.Exit` schedule for self-reload, so the respawned daemon can acquire it cleanly.

**Rationale.** Concurrent verbs on the same source repo MUST NOT interleave with a reload sequence. A `merge_to_master` running mid-build would see an inconsistent worktree; a second `reload` would race the pull.

**Release-before-exit ordering for self-reload.** The lock is released before `os.Exit(75)` is scheduled. If the new daemon comes up before the old daemon's deferred-exit fires, both could hold the lock briefly — but the lock is in-process state, not a filesystem lock, so it dies with the old process. The new daemon's lock starts fresh.

### D10. `task_id` is optional for self-hosting verbs only

`iris-host-bridge`'s "Source-repo path resolution from `task_id`" requirement is MODIFIED to add a narrow carve-out: a verb MAY be designated "self-hosting." Self-hosting verbs MAY omit `task_id`, in which case the daemon discovers its own source repo via `os.Executable()` symlink resolution. The carve-out applies to `iris:reload` in v1.1; future verbs may opt into it if and only if they meet the same self-hosting criteria.

**Rationale.** Keeping the rule tight prevents a future verb author from accidentally inheriting the carve-out. The exception is justified once — for the case where there is no argus task because iris itself is the target.

**Schema enforcement.** The MCP input schema for `iris_reload` lists `task_id` and `path` as both optional but mutually exclusive. The CLI mirrors this with a positional argument.

### D11. Pre-flight and verify hooks are optional but spec'd

Two optional blocks let the project author add custom steps:

- **`[pre_flight]`** runs after iris's built-in pre-flight refusals but before the pull. Used for project-specific checks ("no pending migrations," "no open file handles on the binary," etc.). Iris runs the configured `command` (argv); non-zero exit aborts the reload with the captured output.
- **`[verify]`** runs after the restart mechanism returns, cross-reload only. Used for "is the new daemon healthy" checks. Iris runs the configured `command`; non-zero exit returns an error to the caller but does NOT roll back the restart. The daemon is on the new binary; the operator decides what to do.

**Rationale.** Without these, project authors push verification into the build command and lose the iris-level audit. With them, every step of a reload is structured and inspectable.

**Why no verify for self-reload.** Iris has already exited; nothing in this process can verify the new daemon. The new daemon's `start` codepath is responsible for self-verification.

### D12. Result shape is structured and uniform

Every reload returns a structured result:

- `target_source_repo` (string, canonical absolute path)
- `mode` (string, "self" or "cross")
- `pulled` (bool, true if iris fetched + merged)
- `pre_pull_sha` (string, 40 hex; head before pull, or pre-call HEAD if no_pull)
- `post_pull_sha` (string; head after pull, or same as pre_pull_sha if no_pull)
- `pre_flight_output` (string, captured, empty if no [pre_flight] block)
- `build_output` (string, captured stdout+stderr)
- `restart_mechanism` (string, mirrors the resolved [restart] mechanism)
- `restart_output` (string, captured; empty for `exit_code` and `none`)
- `verify_output` (string, captured, empty if no [verify] block or self-reload)
- `restart_pending` (bool, true for self-reload — restart happens after response, false for cross-reload — restart already happened)
- `warnings` (string[]; e.g., "origin/HEAD unset, defaulted to main")

**Rationale.** A uniform shape means callers, the audit log, and future status tooling all parse the same data. Empty strings rather than null/missing fields keep parsing simple.

### D13. Structured audit log

Every reload (success and failure) appends a JSON line to `~/.iris/reload-history.jsonl`. The line is the result above plus a wall-clock timestamp and the caller identity (argus task_id if present, or "cli" if direct CLI, or "self" if no caller).

**Rationale.** Cheap to add now; expensive to add later. Operators get a forensic trail for every reload. Future `iris status <repo>` reads this file to surface "last reload at <time>" without needing a new storage layer.

### D14. CLI mirror

`iris reload [target] [--no-pull] [--timeout=Ns]` runs the same Go function as the MCP handler.

- `target` (optional positional): one of
  - omitted → self-reload via `os.Executable()`
  - starts with `/` or `~` or `.` → treated as a filesystem path
  - otherwise → treated as an argus task_id
- `--no-pull` → set `no_pull = true`
- `--timeout=Ns` → overrides per-step timeouts globally for this call

The CLI prints the structured result as pretty-printed JSON (matches the other verbs' CLI ergonomics).

## The `.iris.toml` schema (v1)

```toml
# Required: tells iris which schema version this file targets.
# Iris refuses unknown versions. v1 is the only valid value today.
schema_version = 1

# Optional: overrides git's origin/HEAD detection. Useful for repos where
# origin/HEAD is unset or has drifted from the actual default.
default_branch = "main"

# Required: the build step. Iris runs this in the source repo root by default.
[build]
command = ["make", "build"]      # required, argv (no shell)
timeout_seconds = 600             # optional, default 600
working_directory = "."           # optional, default ".", relative to repo root
env = { GOFLAGS = "-mod=vendor" } # optional, key=value pairs prepended to the build environment

# Required: how to bring the new binary into service.
[restart]
mechanism = "exit_code"           # one of: exit_code, launchagent, launchdaemon, signal, exec, none

# For mechanism = "exit_code" (self-managed only):
# code = 75                       # optional, default 75; must be non-zero

# For mechanism = "launchagent" or "launchdaemon":
# label = "com.anutron.iris"      # required

# For mechanism = "signal":
# pid_file = "/var/run/foo.pid"   # required
# signal = "SIGTERM"              # required; iris accepts standard signal names

# For mechanism = "exec":
# command = ["launchctl", "kickstart", "-k", "gui/501/com.anutron.iris"]  # required argv
# timeout_seconds = 30            # optional, default 30

# Optional: extra checks before pull. Non-zero exit aborts the reload.
[pre_flight]
command = ["bin/check-migrations"]
timeout_seconds = 60

# Optional: post-restart health check (cross-reload only; ignored for self).
[verify]
command = ["curl", "-fsS", "http://localhost:8080/health"]
timeout_seconds = 30
```

**Iris's own `.iris.toml`** (lands in this repo as part of this change):

```toml
schema_version = 1
default_branch = "main"

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
```

Six lines (plus blanks). The minimum useful `.iris.toml`.

## Companion verbs

The remaining three verbs in this change are thin readers and validators that piggy-back on the convention and audit log `iris:reload` introduces.

### `iris:validate_config`

Parses + cross-validates a `.iris.toml` file without doing anything else. Used by CI to verify a project's config before merging, by Claude to verify a freshly-authored config, and by humans during debugging.

- **Input:** either `task_id` (resolves to source repo), `path` (filesystem path), or neither (self).
- **Behavior:** runs the same parser + cross-validator that `iris:reload` uses for pre-flight. Returns `{ valid: true|false, errors: [...], warnings: [...], resolved: { schema_version, default_branch, build: {...}, restart: {...}, ... } }`. Each error names the offending field, the line number (if available from the TOML parser), and a remediation hint.
- **No side effects.** No pull. No build. No restart. No audit-log write. Pure parse + report.
- **MCP shape:** `iris_validate_config { task_id?, path? }`.
- **CLI shape:** `iris validate-config [target]`.

### `iris:ls`

Reads `~/.iris/reload-history.jsonl` and projects the managed systems iris has touched. No registry, no scan; the audit log IS the inventory.

- **Input:** no arguments (just lists everything iris has seen).
- **Behavior:** reads the audit log, dedupes by `target_source_repo`, returns a list of `{ source_repo, last_reload_at, last_outcome, last_mode, last_pre_pull_sha, last_post_pull_sha, total_reload_count, total_failure_count }`. Sorted by `last_reload_at` descending.
- **MCP shape:** `iris_ls { limit?, since? }`. Optional limit (default 50) and since-timestamp filter.
- **CLI shape:** `iris ls [--limit=N] [--since=RFC3339-TIMESTAMP]`.
- **Empty case.** If `~/.iris/reload-history.jsonl` does not exist, returns an empty list with a structured warning ("no reloads recorded yet").

### `iris:status`

For one managed system, reports the current convention + git state + last-reload outcome. Equivalent of `iris ls` but focused on a single target and richer per-target.

- **Input:** either `task_id`, `path`, or neither (self).
- **Behavior:** resolves the source repo, reads + validates `.iris.toml` (without side effects), captures git state (`HEAD`, `default branch`, working-tree-clean status, `origin/<default-branch>` SHA), reads the most recent entry for this source repo from the audit log. Returns one structured object.
- **Drift detection.** If `HEAD ≠ last reload's `post_pull_sha``, includes a `drift: true` flag indicating local commits since the last reload. If `HEAD == origin/<default-branch>`, includes `up_to_date: true`. Otherwise `up_to_date: false` (origin has commits not yet pulled).
- **MCP shape:** `iris_status { task_id?, path? }`.
- **CLI shape:** `iris status [target]`.

### Why these three together

- **All read-only.** No locks held (other than briefly to avoid races with an in-flight reload appending to the audit log). Cheap to call repeatedly.
- **All share the audit-log reader.** Implementation is a single `internal/verbs/audit.go` module with a write path (used by reload) and three read shapes (used by ls and status).
- **All share the `.iris.toml` parser.** Implementation is a single `internal/config/iris_toml.go` module with parse + cross-validate functions, called by reload, validate_config, status.
- **All inherit the `task_id`-optional carve-out.** Self-management means iris can introspect or validate its own state without an argus task. Same `iris-host-bridge` carve-out covers all four verbs.

## Acceptance criteria

### Pre-flight refusals

- it should refuse when the working tree has uncommitted changes, naming the dirty paths
- it should refuse when the current branch is not the resolved default branch, naming both
- it should refuse when the source repo is not a git repository, naming the path
- it should refuse when origin remote is missing or unreachable
- it should refuse when `.iris.toml` is missing at the repo root, naming the expected path
- it should refuse when `.iris.toml` parses successfully but `schema_version` is missing, not 1, or not an integer
- it should refuse when `.iris.toml` declares a `[restart] mechanism` that has fields belonging to a different mechanism (e.g., launchagent + pid_file), naming the conflict
- it should refuse when `[restart] mechanism = "exit_code"` is declared in a `.iris.toml` whose source repo is not iris's own deployed source repo (exit_code is self-only)
- it should refuse when `[restart] mechanism = "exit_code"` is declared with `code = 0` (must be non-zero)

### Pull behavior

- it should fetch and fast-forward-only merge origin/<default-branch> before invoking build, unless `no_pull` is true
- it should skip the pull when `no_pull` is true and record `pulled = false` in the result
- it should refuse with a structured error when fast-forward is not possible (divergent history), naming local and remote SHAs
- it should hold the per-source-repo lock from immediately after pre-flight through the end of restart (cross) or response flush (self)

### Build step

- it should run the configured `[build] command` in the configured `working_directory` (defaulting to the source repo root) with the configured `env` merged into the process environment
- it should capture combined stdout+stderr verbatim and include it in `build_output`
- it should enforce `[build] timeout_seconds` (default 600), killing the build process group on timeout and returning a structured error
- it should return a structured error containing the captured output when build exits non-zero, and NOT proceed to restart

### Pre-flight hook

- it should run the `[pre_flight] command` after iris's built-in pre-flight refusals but before pull, when the block is present
- it should abort the reload with the captured output when `[pre_flight] command` exits non-zero
- it should enforce `[pre_flight] timeout_seconds` (default 60), killing the process group on timeout

### Self-vs-cross detection

- it should classify the reload as self when the resolved target source repo equals `os.Executable()`'s canonical git root
- it should set `mode = "self"` and `restart_pending = true` for self-reload
- it should set `mode = "cross"` and `restart_pending = false` for cross-reload

### Restart mechanism dispatch

- it should schedule `os.Exit(<code>)` (default 75) after the response is flushed, for `mechanism = "exit_code"` self-reload, releasing the lock first
- it should run `launchctl kickstart -k gui/<uid>/<label>` for `mechanism = "launchagent"` and include launchctl's stdout+stderr in `restart_output`
- it should run `launchctl kickstart -k system/<label>` for `mechanism = "launchdaemon"` and warn in `warnings` when iris is not running as root
- it should read PID from `pid_file`, send the configured `signal`, and capture any error from the kill syscall in `restart_output`, for `mechanism = "signal"`
- it should run the configured `command` argv for `mechanism = "exec"`, capturing stdout+stderr in `restart_output`, with `timeout_seconds` (default 30) enforcement
- it should do nothing for `mechanism = "none"` and set `restart_output = ""`

### Post-restart verify hook (cross-reload only)

- it should run the `[verify] command` after a successful cross-reload restart, when the block is present
- it should return a structured error containing the verify output when `[verify] command` exits non-zero, without rolling back the restart
- it should skip `[verify]` entirely for self-reload (iris has already exited; nothing in-process can verify)
- it should enforce `[verify] timeout_seconds` (default 30)

### `task_id` optionality

- it should accept a call with no `task_id` and no `path`, discovering its source repo via `os.Executable()` (self-reload only path)
- it should accept a `task_id` and resolve the source repo via the existing `verbs.Resolve` (argus task → worktree → git common-dir)
- it should accept a `path` and resolve the source repo via `git -C <path> rev-parse --git-common-dir`
- it should reject a call where both `task_id` and `path` are supplied (ambiguous)

### Argus project allowlist enforcement (cross-reload only)

- it should refuse a cross-reload whose resolved source repo is not on the argus project allowlist, naming the rejected path
- it should NOT consult the argus project allowlist for self-reload (iris's own repo is implicit)

### Audit log

- it should append one JSON line per reload (success and failure) to `~/.iris/reload-history.jsonl`
- it should include wall-clock timestamp, caller identity, and the full structured result in each audit entry
- it should NOT crash the reload on audit-write failure (best-effort, log the error to the daemon log)

### CLI mirror

- it should run the same `verbs.Reload` Go function as the MCP handler when invoked as `iris reload [target] [--no-pull]` from any shell
- it should default to self-reload when invoked with no positional argument
- it should treat a positional argument starting with `/`, `~`, or `.` as a path
- it should treat any other positional argument as an argus task_id
- it should print the structured result as pretty-printed JSON

### `iris:validate_config` behavior

- it should parse the `.iris.toml` at the resolved source repo's root and return `valid: true` when all required fields are present and cross-validations pass
- it should return `valid: false` with one or more structured errors when the file is missing, malformed TOML, or fails cross-validation, naming the offending field and giving a remediation hint
- it should report line numbers for parser-level errors when the TOML library provides them
- it should NOT run pull, build, restart, or any audit-log write
- it should run from a CLI invocation (`iris validate-config [target]`) using the same Go function as the MCP handler

### `iris:ls` behavior

- it should read `~/.iris/reload-history.jsonl` and return one entry per distinct `target_source_repo`, sorted by `last_reload_at` descending
- it should aggregate `total_reload_count` and `total_failure_count` across all entries for each source repo
- it should return an empty list with a structured warning when the audit log is missing or empty
- it should honor an optional `limit` (default 50) and `since` (RFC3339 timestamp) filter
- it should NOT modify any state, lock any source repo, or invoke any verb beyond reading the audit log

### `iris:status` behavior

- it should resolve the target source repo (from `task_id`, `path`, or self), read `.iris.toml`, capture HEAD + default-branch + working-tree-clean state, and return one structured object
- it should include `drift: true` when local HEAD differs from the last reload's `post_pull_sha`
- it should include `up_to_date: true` when HEAD equals `origin/<default-branch>` and `up_to_date: false` otherwise
- it should return `last_reload: null` when no audit log entry exists for the source repo
- it should NOT modify any state or run pull/build/restart

## Trust and security model

- **`.iris.toml` is in the project repo.** An attacker who can write to the repo can rewrite the build or restart command. This is bounded by the existing argus project allowlist for cross-reload (only operator-approved repos can be reloaded at all). For self-reload, the iris repo itself must be protected at the git/filesystem level; iris cannot improve on that.
- **No shell expansion.** All commands are argv lists. `command = ["make", "build"]`, never `command = "make build"`. This prevents shell-injection footguns.
- **Argv values are taken verbatim.** Iris does not interpolate environment variables into argv. If a project author wants `${HOME}` expansion, they put the expanded path in the file.
- **Lock is in-process.** The per-source-repo lock is a Go `sync.Mutex` keyed by canonical path. It dies with the iris process. A new iris (post-respawn) starts with a fresh lock map. This is intentional: the new daemon's lock state should not inherit from the old one.

## Observability

- **Audit log** at `~/.iris/reload-history.jsonl`, one JSON line per reload. Append-only, never rotated by iris in v1.1. Operator may rotate manually.
- **Daemon log** at `~/.iris/launchd.log` (existing) captures iris's structured log lines for each step (parse, lock, pull, build, restart). Step transitions logged at INFO; failures at WARN or ERROR.
- **Result returned to caller** is the same JSON object that lands in the audit log (minus the wall-clock-timestamp + caller-identity fields, which are added at audit time).

## Forward compatibility

The v1 schema is shaped so the following extensions can land without breaking v1 `.iris.toml` files:

- **`[[targets]]` array for monorepos** — schema_version 2 could allow either the flat `[build]`/`[restart]` (one daemon per repo) or a `[[targets]]` list. v1 files remain valid as "one target with default name."
- **`iris validate-config <repo>`** — new verb that runs the parse + cross-validation paths against a `.iris.toml` without doing pull or build. Implementation reuses the same parser this change introduces.
- **`iris ls`** — new verb that reads `~/.iris/reload-history.jsonl` and surfaces "managed systems iris has reloaded recently." No registry; just history.
- **`iris status <repo>`** — new verb that reads the audit log plus the source repo's git state to show "last reload at <time>, deployed SHA X, current HEAD Y."
- **`iris reload --dry-run`** — runs pre-flight + parse + plan output without invoking build or restart. The result shape is already structured enough to support this; the dry-run path just returns the plan instead of executing.
- **New restart mechanisms** — adding `systemd`, `runit`, etc. is non-breaking (existing mechanisms keep working).
- **`[build.cache]` block** — future optimization to avoid rebuilding when source hasn't changed. Schema extension only.
- **`[restart.health_check_url]`** — future shorthand for the common HTTP-health-check `[verify]` block. Schema extension only.

## Risks and trade-offs

- **Risk:** Script hangs (e.g., `make build` deadlocks on a dependency). → Mitigation: per-step timeouts enforced by iris, killing the process group on timeout.
- **Risk:** Self-reload exits before the response is flushed. → Mitigation: response writer is explicitly flushed; a brief delay (~100 ms) in a deferred goroutine gives the kernel time to ACK before `os.Exit` fires.
- **Risk:** New binary panics on startup; LaunchAgent enters a respawn loop. → Mitigation: macOS LaunchAgent already throttles respawns (10-second minimum interval, escalating). Operator notices via `iris status` failing and falls back to re-running `setup.sh` to install a known-good binary.
- **Risk:** Exit code 75 collides with a real iris bug that exits 75. → Mitigation: 75 is `EX_TEMPFAIL` from sysexits.h; iris's own non-reload codepaths use 0 or 1. If a collision emerges, switch the default to a higher unused sysexits value. The code is configurable per `.iris.toml`.
- **Risk:** Audit log grows unbounded. → Mitigation: documented in `README.md`; operator rotates manually. Adding internal rotation in a future version is non-breaking.
- **Risk:** Project author writes a build command that does something destructive (rm -rf, push to wrong remote, etc.). → Mitigation: same trust model as today — iris runs as the operator. The argus project allowlist bounds *which* repos can be reloaded; what they do internally is the project's responsibility.
- **Trade-off:** Declarative schema means a project with a genuinely novel restart mechanism must use `mechanism = "exec"` with a custom argv. The structured shape is preserved (iris still owns lock/pull/build/audit) but the restart step itself is opaque. Acceptable.
- **Trade-off:** Five restart mechanisms is more surface area than "just run a script." Operator and Claude both have to know which mechanism applies. Documented in the README; the schema is small enough that the cost is real but small.

## Migration plan

1. Ship this change (`add-daemon-self-management`) on a feature branch. Includes:
   - `internal/config/iris_toml.go` (TOML parser + cross-validator, used by all four verbs)
   - `internal/verbs/audit.go` (audit-log writer + reader, used by reload + ls + status)
   - `internal/verbs/reload.go` + tests
   - `internal/verbs/validate_config.go` + tests
   - `internal/verbs/ls.go` + tests
   - `internal/verbs/status.go` + tests
   - `internal/mcp/handler_reload.go`, `handler_validate_config.go`, `handler_ls.go`, `handler_status.go` + tests
   - `cmd/iris/reload.go`, `validate_config.go`, `ls.go`, `status.go` (Cobra subcommands)
   - Registration of 4 new tools in `internal/daemon/run.go`
   - `.iris.toml` for iris itself (the six-line example)
   - README section on `.iris.toml`, the four self-management verbs, and the audit log
   - `go.mod` dependency on `github.com/BurntSushi/toml`
2. Dogfood: open a no-op PR against iris's main; merge it; call `iris validate-config` against iris's own repo (should pass); call `iris reload` against the task; verify LaunchAgent respawns; call `iris status` to confirm the new binary's git SHA matches the post-pull SHA; call `iris ls` to confirm the audit-log entry landed.
3. After dogfood, archive this OpenSpec change.
4. **Rollback path.** Revert this change. The existing v1.0 verbs continue working. The `.iris.toml` file in the iris repo can stay (it's a no-op without the verb). The audit log at `~/.iris/reload-history.jsonl` can be deleted; nothing else depends on it.

## Open questions

None at design time. All design decisions resolved above.
