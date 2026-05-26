---
session: overnight-iris-v1-verbs
branch: argus/handoff-bootstrap-iris-plugin
date: 2026-05-26
mode: manual (skill prerequisites not met)
---

# Spec audit (manual) – 2026-05-26

## Why this is manual

`/spec-audit` requires base specs at `openspec/specs/<capability>/spec.md` to run. This project is in v1 bootstrap state — every capability lives in an active change folder (`openspec/changes/<change>/specs/<capability>/spec.md`) and they all archive together at the end. The skill explicitly refuses to run in that state, recommending `/spec-recommender` instead, which doesn't fit here either.

So I did the audit manually: enumerate every behavioral `.go` file, map it to a covering requirement somewhere in the six active changes, and flag any gap.

## Scope

Six active changes provide the spec corpus:

- `bootstrap-iris-plugin` (capabilities `iris-host-bridge`, `iris-merge-to-master`)
- `add-push-verb` (`iris-push`)
- `add-gh-pr-create-verb` (`iris-gh-pr-create`)
- `add-gh-pr-merge-verb` (`iris-gh-pr-merge`)
- `add-run-build-verb` (`iris-run-build`)
- `add-complete-task-verb` (`iris-complete-task`)

Behavioral code files audited: 40 across `cmd/iris/`, `internal/argus/`, `internal/config/`, `internal/daemon/`, `internal/mcp/`, `internal/verbs/`.

## Coverage map

| File | Covering capability / requirement |
|------|------------------------------------|
| `cmd/iris/main.go` | `iris-host-bridge` § Daemon lifecycle (single binary) |
| `cmd/iris/start.go`, `stop.go`, `status.go` | `iris-host-bridge` § Daemon lifecycle |
| `cmd/iris/merge_to_master.go` | `iris-merge-to-master` § Direct CLI invocation |
| `cmd/iris/push.go` | `iris-push` § Direct CLI invocation |
| `cmd/iris/gh_pr_create.go` | `iris-gh-pr-create` § Direct CLI invocation |
| `cmd/iris/gh_pr_merge.go` | `iris-gh-pr-merge` § Direct CLI invocation |
| `cmd/iris/run_build.go` | `iris-run-build` § Direct CLI invocation |
| `cmd/iris/complete_task.go` | `iris-complete-task` § Direct CLI invocation |
| `internal/argus/client.go` | `iris-host-bridge` § Scope-token authentication |
| `internal/argus/errors.go` | implementation detail (HTTPError type) |
| `internal/argus/linkstate.go` | `iris-host-bridge` § Argus-restart recovery (LinkHealthy/LinkRecovering/LinkDown) |
| `internal/argus/mcp.go` | `iris-host-bridge` § MCP tool registration with heartbeat |
| `internal/argus/projects.go` | `iris-host-bridge` § Source-repo path resolution (allowlist lookup) |
| `internal/argus/recovery.go` | `iris-host-bridge` § Argus-restart recovery (single-flight) |
| `internal/argus/socket.go` | `iris-host-bridge` § Argus-restart recovery (PortsClient) |
| `internal/argus/tasks.go` | `iris-host-bridge` § Source-repo path resolution (GetTask) + `iris-complete-task` scenarios for SetTaskStatus / ArchiveTask |
| `internal/argus/watcher.go` | `iris-host-bridge` § Argus-restart recovery (watcher) |
| `internal/config/config.go` | `iris-host-bridge` § Daemon lifecycle + State directory |
| `internal/daemon/run.go` | `iris-host-bridge` § Daemon lifecycle (assembly) |
| `internal/mcp/envelope.go`, `handler.go` | `iris-host-bridge` § Inbound MCP callback (shape) |
| `internal/mcp/handler_merge_to_master.go` | `iris-merge-to-master` scenarios |
| `internal/mcp/handler_push.go` | `iris-push` scenarios |
| `internal/mcp/handler_gh_pr_create.go` | `iris-gh-pr-create` scenarios |
| `internal/mcp/handler_gh_pr_merge.go` | `iris-gh-pr-merge` scenarios |
| `internal/mcp/handler_run_build.go` | `iris-run-build` scenarios |
| `internal/mcp/handler_complete_task.go` | `iris-complete-task` scenarios |
| `internal/mcp/registrar.go` | `iris-host-bridge` § MCP tool registration with heartbeat |
| `internal/mcp/server.go` | `iris-host-bridge` § Daemon lifecycle + Scope-token auth + Inbound callback body limit |
| `internal/verbs/build_locks.go` | `iris-run-build` § Concurrent builds in different worktrees + same worktree serialize |
| `internal/verbs/complete_task.go` | `iris-complete-task` full spec |
| `internal/verbs/gh_pr_create.go` | `iris-gh-pr-create` full spec |
| `internal/verbs/gh_pr_merge.go` | `iris-gh-pr-merge` full spec |
| `internal/verbs/locks.go` | `iris-host-bridge` § Per-source-repo mutex on git operations |
| `internal/verbs/merge_to_master.go` (incl. `mergeToMasterLocked`) | `iris-merge-to-master` full spec + `iris-complete-task` § Three git sub-steps share one lock |
| `internal/verbs/push.go` | `iris-push` full spec |
| `internal/verbs/resolve.go` | `iris-host-bridge` § Source-repo path resolution |
| `internal/verbs/run_build.go` | `iris-run-build` full spec |

## Gaps found

### 1. `WorktreePath` canonicalization (auto-fixed)

`internal/verbs/resolve.go` now canonicalizes the worktree path the same way it canonicalizes the source-repo path. The behavior was added in ralph-review loop 1 (commit `c6cb65b`) but the host-bridge spec only described source-repo canonicalization.

**Fix applied:** added language to `iris-host-bridge` § Source-repo path resolution requiring both paths to be canonicalized; added a `Resolved paths are canonical on both sides` scenario making the macOS `/var` ↔ `/private/var` invariant explicit.

## Skipped (not gaps)

- **`internal/argus/errors.go`** — implementation detail, the `HTTPError` type is internal plumbing not user-visible behavior.
- **`internal/mcp/handler.go`, `envelope.go`** — type definitions for the handler/envelope shape; covered by the iris-host-bridge MCP requirement implicitly. Adding a separate "handler interface" requirement would be over-spec'd.
- **`SetTaskStatus` and `ArchiveTask` on the argus client** — referenced by `iris-complete-task` scenarios via their HTTP paths (`POST /api/tasks/<id>/status` and `/archive`); the verb owns the contract, the client just transports.

## Validation gates after the spec edit

- `go vet ./...` clean
- `openspec validate --all --strict` passes (6 changes valid)
- `make test` green under `-race`

## Recommendation

Coverage is healthy. The single gap was a loop-1 omission that's now closed. When the stack archives, all six changes merge into base specs at `openspec/specs/<capability>/spec.md` and a real `/spec-audit` will run cleanly going forward.
