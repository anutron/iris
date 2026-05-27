# Add source-repo utility verbs

## Why

v1.0 gives iris a strong git-mutating surface (`merge_to_master`, `push`) and a strong gh surface (`gh_pr_create`, `gh_pr_merge`). Three small git operations sit naturally next to those but aren't yet exposed: fetching origin, deleting a remote branch, and creating a release tag. Each one is a host-side privileged operation an agent in a sandbox cannot do directly (origin's ssh keys live on the host), each benefits from the existing per-source-repo lock, and each completes the realistic surface a release-shipping agent needs.

## What Changes

- **New `iris:fetch` verb.** Shells out to `git fetch origin` under the per-source-repo lock. Returns the list of refs updated.
- **New `iris:branch_delete_remote <branch>` verb.** Shells out to `git push origin :<branch>`. Refuses to delete the default branch. Returns the remote SHA at the time of deletion.
- **New `iris:tag <tag>` verb.** Creates an annotated tag at HEAD of the default branch and pushes it to origin. Refuses if tag already exists. Returns the tag SHA and remote-push status.

All three follow the v1.0 verb shape: `task_id` resolves the source repo; git runs from inside that repo; the per-source-repo lock is held across the operation; output is captured and structured.

## Capabilities

### New Capabilities

- `iris-fetch`: `iris:fetch` verb — fetch origin under the source-repo lock.
- `iris-branch-delete-remote`: `iris:branch_delete_remote` verb — delete a remote branch with default-branch refusal.
- `iris-tag`: `iris:tag` verb — create an annotated tag at HEAD of the default branch and push it.

### Modified Capabilities

None.

## Impact

- New Go files: `internal/verbs/fetch.go`, `branch_delete_remote.go`, `tag.go`, matching `internal/mcp/handler_*.go`, and matching `cmd/iris/*.go` Cobra subcommands.
- Registration in `internal/daemon/run.go`: 3 new tool definitions.
- No new dependencies. Pure git shellout via existing patterns.
- No changes to `iris-host-bridge`. All three verbs require `task_id` (not self-hosting).
