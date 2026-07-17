## Why

`iris:push` and `iris:fetch` set no git-op timeout of their own — the underlying `git push`/`git fetch` subprocess inherits the inbound MCP request's context (`r.Context()`), which argus's outbound HTTP client owns. When a push or fetch runs long (a large or far-diverged branch), argus's client eventually gives up and closes the connection; iris's server detects that and cancels `r.Context()`, which kills the in-flight git subprocess mid-transfer. The caller sees an opaque "context deadline exceeded" and cannot tell whether that was iris's own deadline, an auth failure, a network failure, or something else — so it doesn't know whether to retry, wait, or fall back to a manual push.

## What Changes

- `iris:push` and `iris:fetch` run their git push/fetch subprocess under iris's own `context.WithTimeout` (detached from the inbound request's cancellation via `context.WithoutCancel`), so an inbound request context dying (argus's client giving up) no longer kills an otherwise-healthy git transfer mid-flight.
- The timeout is configurable via a new shared `.iris.toml` field, `git_transfer_timeout_seconds`, defaulting to 300s (comfortably larger than today's unbounded-but-fragile effective ceiling). All other git invocations in these verbs (rev-parse, remote lookups, ref snapshots) are unaffected — they're fast local operations and keep using the caller's context as before.
- Push/fetch failures are classified into a distinguishable reason (`timeout`, `auth_failure`, `network_failure`, `other_failure`) via a new typed `*verbs.GitTransferError`, surfaced through the existing error-message text (no response-shape changes) so a caller can tell "iris's own deadline fired" apart from "git rejected the credentials" apart from "the network dropped" apart from "non-fast-forward / other git failure" — and decide whether to retry, wait, or fall back to a manual push.
- Not breaking: the new config field is optional (defaults applied when absent), and existing error-message substrings callers already match on (`"default branch"`, `"push"`, `"fetch"`, etc.) are preserved.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `iris-push`: the verb's `git push` runs under a configurable, iris-owned timeout decoupled from the inbound request context; failures are classified (timeout/auth/network/other).
- `iris-fetch`: the verb's `git fetch` runs under the same configurable, iris-owned timeout mechanism; failures are classified identically.
- `iris-host-bridge`: documents the general pattern (config field placement/taxonomy, context-decoupling mechanism) so future git-network verbs can adopt it consistently.

## Impact

- `internal/config/iris_toml.go` — add `GitTransferTimeoutSeconds` (top-level, `kind:"shared"`), `DefaultGitTransferTimeoutSeconds` constant, validation (non-negative), and a `ResolvedGitTransferTimeoutSeconds()` helper.
- `internal/config/iris_toml_taxonomy_test.go` — add the new field to the pinned `shared` field lists.
- `internal/verbs/git_transfer.go` (new) — `GitTransferError` type, `GitTransferReason` enum, `classifyGitFailure`, `runGitTransfer` (context-detach + timeout + `WaitDelay` wrapper around an `exec.Cmd`), `gitTransferTimeout` (loads the configured/default duration for a source repo), `IsGitTransferTimeout` helper, `postTransferReadTimeout` constant.
- `internal/verbs/push.go`, `internal/verbs/fetch.go` — route the actual `git push` / `git fetch` invocation through `runGitTransfer` with the resolved timeout; also detach the post-success result read-back (push's rev-parse, fetch's post-fetch snapshot) from the caller's context on a short fixed grace period. Git calls that happen before any mutation is attempted stay on the caller's context, unchanged.
- `internal/mcp/handler_push.go`, `internal/mcp/handler_fetch.go` — no code changes required (the classification flows through the existing `fmt.Sprintf("iris:push: %v", err)` / `iris:fetch: %v` formatting automatically since it's carried by the error's `Error()` string).
- `cmd/iris/push.go`, `cmd/iris/fetch.go` — no code changes required (same reasoning; verified via `go build`/`go test`).
- `internal/daemon/run.go` — update the `iris_push` / `iris_fetch` MCP tool descriptions to mention the configurable timeout and error classification, for agent discoverability.
- `README.md`, `claude/skills/iris/SKILL.md` — document the new `.iris.toml` field and the timeout/classification behavior, matching the existing documentation convention for every field/verb.
- No data, migration, or cross-service impact.
