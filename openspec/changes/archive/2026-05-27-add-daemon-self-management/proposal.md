# Add daemon self-management verbs

## Why

Iris v1.0 ships six verbs that operate on agent-supplied argus tasks. None of them upgrade a running daemon, introspect a managed daemon's state, or surface what iris has been doing recently. The only path today is re-running `setup.sh` for every roll, plus eyeballing `~/.iris/launchd.log` for forensics. The natural workflow ("merge a code-only PR, then live-swap the binary, then verify it took") has no first-class verbs.

This change introduces a coherent daemon-self-management subsystem: a declarative `.iris.toml` convention any iris-managed daemon adopts, the `iris:reload` verb that drives a live upgrade through that convention, and three companion read verbs (`validate_config`, `ls`, `status`) so operators and downstream tooling can introspect what iris has been doing.

Iris itself becomes the first member of the "managed system" concept, and the design generalizes to any future LaunchAgent-style daemon iris is asked to manage without iris changes.

## What Changes

- **New convention:** `<repo-root>/.iris.toml` declarative file describing how to upgrade a managed daemon. Required fields: `schema_version`, `[build]`, `[restart]`. Optional: `default_branch`, `[pre_flight]`, `[verify]`.
- **New `iris:reload` verb.** Pulls (ff-only), runs the build, dispatches to the configured restart mechanism. Self-vs-cross detection is automatic.
- **Self-reload uses an `exit_code` mechanism** (default `code = 75`): iris responds to the caller, releases the lock, then exits non-zero so the LaunchAgent's `KeepAlive: SuccessfulExit=false` respawns from the new binary.
- **Cross-reload supports six restart mechanisms:** `exit_code` (self only), `launchagent`, `launchdaemon`, `signal`, `exec`, `none`. The `exec` mechanism is the long-tail escape hatch.
- **`task_id` becomes optional for self-hosting verbs only** (`iris:reload`, `iris:validate_config`, `iris:ls`, `iris:status`). When the caller wants iris to operate on itself, no argus task exists. Iris discovers its own source repo via `os.Executable()` symlink chase.
- **New audit log:** `~/.iris/reload-history.jsonl`. One JSON line per reload (success and failure). Forensic trail for every upgrade.
- **New `iris:validate_config` verb.** Parses + cross-validates a `.iris.toml` without running anything. For CI checks and Claude-authored configs.
- **New `iris:ls` verb.** Reads the reload-history audit log and surfaces managed systems iris has reloaded recently. No registry; just history.
- **New `iris:status` verb.** For a given source repo (by path or task_id, or implicit self), reports the resolved `.iris.toml`, default branch, current HEAD SHA, last reload SHA + timestamp + outcome from the audit log.
- **Iris's own `.iris.toml`** lands in this repo as part of this change. Six meaningful lines.
- **`iris-host-bridge` MODIFIED**: a small carve-out to the "every verb resolves source repo from `task_id`" requirement, allowing self-hosting verbs to omit `task_id` and discover their source repo via `os.Executable()`.

## Capabilities

### New Capabilities

- `iris-reload`: `iris:reload` verb behavior, the `.iris.toml` schema, self-vs-cross detection, the exit-code respawn mechanism, the six cross-reload mechanisms, the audit log.
- `iris-validate-config`: `iris:validate_config` verb — parse a `.iris.toml`, cross-validate mechanism-specific fields, report errors with line numbers and remediation hints.
- `iris-ls`: `iris:ls` verb — read the reload-history audit log and project managed systems with last-reload metadata.
- `iris-status`: `iris:status` verb — for one managed system, report `.iris.toml` resolution, git state, and last reload outcome.

### Modified Capabilities

- `iris-host-bridge`: introduce a narrow carve-out to the "Source-repo path resolution from `task_id`" requirement, allowing self-hosting verbs to omit `task_id` and discover their source repo via `os.Executable()`.

## Impact

- New Go files: `internal/config/iris_toml.go` (parser + validator), `internal/verbs/reload.go`, `internal/verbs/validate_config.go`, `internal/verbs/ls.go`, `internal/verbs/status.go`, `internal/verbs/audit.go` (audit-log writer/reader), matching `internal/mcp/handler_*.go` files, and matching `cmd/iris/*.go` Cobra subcommands.
- Registration in `internal/daemon/run.go`: 4 new tool definitions.
- New repo-root file: `.iris.toml` in iris's own source tree (six lines), demonstrating the convention.
- New dependency: `github.com/BurntSushi/toml` for schema parsing.
- New runtime contract: `KeepAlive: SuccessfulExit=false` is load-bearing for self-reload (was previously load-bearing for `launchctl bootout` only). `setup.sh` and the host-bridge spec already document this; the reload spec relies on it explicitly.
- New filesystem footprint: `~/.iris/reload-history.jsonl` (append-only audit log; operator-rotated).
