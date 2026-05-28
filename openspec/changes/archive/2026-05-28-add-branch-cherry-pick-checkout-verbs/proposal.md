# Add branch/cherry-pick/checkout verbs

## Why

The "branch off upstream + cherry-pick this commit + open PR" flow (the hotfix-from-worktree-commit pattern) is currently not expressible through iris. A sandboxed agent that wants to take a commit from its argus worktree and put it on a fresh PR branch off `origin/master` has to escape the iris surface and run `git` directly in the source repo — which the sandbox blocks. That gap forces the human to step in mid-flow.

The three verbs proposed here close it. The cherry-pick verb is the load-bearing one; the other two pair with it and also cover a recurring recovery gap (source repo stuck mid-switch with no in-sandbox path to fix it).

## What Changes

- **New `iris:branch_create <name> <base_ref>` verb.** Creates a branch in the source repo from an arbitrary ref. Does NOT check out. Refuses default branch, existing branch, and flag-smuggling refnames.
- **New `iris:cherry_pick <commit> <target_branch>` verb.** Checkouts `target_branch` and runs `git cherry-pick <commit>` under the per-source-repo lock. On conflict, aborts cleanly and returns a structured error. Refuses cherry-picking onto the default branch.
- **New `iris:checkout <branch>` verb.** Switches the source repo to `branch`. With `force=true`, aborts any in-progress merge/cherry-pick/rebase and discards working-tree changes before switching — the "get me unstuck" recovery path.

All three follow the existing verb shape: `task_id` resolves the source repo; git runs from inside it; the per-source-repo lock is held across the operation; output is captured and structured.

## Capabilities

### New Capabilities

- `iris-branch-create`: `iris:branch_create` verb — create a branch in the source repo from an arbitrary ref.
- `iris-cherry-pick`: `iris:cherry_pick` verb — cherry-pick a commit onto a target branch under the source-repo lock with abort-on-conflict.
- `iris-checkout`: `iris:checkout` verb — switch the source repo to a branch, with a force mode that aborts in-progress operations and discards working-tree changes.

### Modified Capabilities

None.

## Impact

- New Go files: `internal/verbs/branch_create.go`, `cherry_pick.go`, `checkout.go`, matching `internal/mcp/handler_*.go`, and matching `cmd/iris/*.go` Cobra subcommands.
- Registration in `internal/daemon/run.go`: 3 new tool definitions.
- No new dependencies. Pure git shellout via existing patterns.
- No changes to `iris-host-bridge`. All three verbs require `task_id`.
- SKETCH.md "future verbs" list shrinks by three entries.
