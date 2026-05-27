## Why

Iris already provides `iris:reload` to update an already-merged commit on the source repo's default branch, build, and restart. But the common dogfooding loop is "I'm in an argus worktree, I've made a commit, and I want it live on the source repo's currently-checked-out branch RIGHT NOW without going through GitHub". Today the operator has to either (a) push and rely on `iris:reload`'s default-branch pull (only works for default-branch flows), or (b) leave the worktree, run git manually in the source repo, then invoke `iris:reload`. `iris:publish` collapses the inner loop: from a worktree, point the source repo at the worktree's HEAD, then build and restart.

This is the v1.2 surface – v1.0 shipped the verb suite, v1.1 added daemon self-management + PR utilities; v1.2 adds the worktree→source-repo sync primitive.

## What Changes

- New verb `iris:publish <task_id> [--branch=X] [--push] [--reset]` exposed as both MCP tool and CLI subcommand.
- Default behavior (ff-only): fast-forward target branch in source repo to worktree's HEAD, then build + restart via reload internals.
- `--reset` opts into `git reset --hard <worktree-sha>` (ref + working tree atomic).
- `--push` adds `git push origin <branch>` (using the same guardrails as v1.0 `iris:push`).
- v1.2 constraint: target branch must equal source repo's currently-checked-out branch. Refuse otherwise.
- Always rebuilds + restarts. No `--no-rebuild` / `--no-restart` flags.
- Reuses `runBuildBlock`, `dispatchRestart`, and audit-log writer from `internal/verbs/reload.go`. Audit entries use `mode: "publish"`.

## Capabilities

### New Capabilities

- `iris-publish`: defines the publish verb shape, pre-flight refusals, fast-forward vs reset behavior, optional push, delegation to reload's build+restart, audit entry shape, and CLI mirroring.

### Modified Capabilities

- (none) – publish delegates to `iris-reload`'s exported build/restart helpers but does not change `iris-reload`'s spec.

## Impact

- New file `internal/verbs/publish.go` with `Publish(ctx, client, PublishInput) (*PublishResult, error)`.
- New tests `internal/verbs/publish_test.go` exercising ff-only, reset, push, refusals, audit, build+restart delegation, against real git tempdirs with bare-origin remotes.
- New cobra subcommand `iris publish <task_id>` wired in `cmd/iris/main.go`.
- New MCP tool registration in the daemon's tool list.
- `internal/verbs/audit.go::AuditEntry.Mode` now also accepts the literal value `"publish"` (schema is open – readers ignore unknown values – so no AuditEntry field changes).
- README documents the verb and its three flags.
- The reload internals (`runBuildBlock`, `dispatchRestart`, `AppendAuditBestEffort`) are reused as-is; if signatures need adjusting for reuse, the change is mechanical (lowercasing them is fine since they all sit in `package verbs`).
