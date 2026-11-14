## Why

Sketch's dogfood server runs from a host checkout that must stay on the `dogfood_branch` composite. `iris_gh_pr_merge` currently checks the host out to the default branch and runs `git pull --ff-only` after the remote merge, with no source-repo lock, no lease check and no timeout. A worker mid-smoke-test watched the server reload under it to main's code. The same class of side effect exists, deliberately, in other verbs that move the host's working branch (`iris_checkout`, `iris_cherry_pick`, `iris_merge_to_master`, `iris_complete_task`) without consulting the dogfood lease.

## What Changes

- `iris_gh_pr_merge` no longer checks out another branch. After the remote merge it fetches (bounded by the git transfer timeout, under the source-repo lock), then updates the local default branch: fast-forward the ref without a checkout when the host is on any other branch; `merge --ff-only` when the host is already on the default branch. A diverged or worktree-held default ref is left alone with a warning. It never leaves the dogfood branch (or any non-default branch).
- Verbs that deliberately move or modify the dogfood branch (`iris_checkout`, `iris_cherry_pick`, `iris_merge_to_master`, `iris_complete_task`) consult the dogfood lease. When the move would change what dogfood serves and another task holds the lease, they refuse, unless `lease_override=true` (warns first and notifies the holder). `iris_complete_task` does not accept the override.
- `iris_tag`, `iris_ship_feature`, `iris_merge_to_branch` are audited and documented as not touching the host checkout (tag/merge_to_branch/ship use refs or scratch worktrees).
- `iris_status` reports `dogfood_branch` and, when the host is on a different branch, a `host_off_dogfood` flag plus a warning.
- Skill docs updated.

## Capabilities

### Modified Capabilities

- `iris-gh-pr-merge`: post-merge local sync no longer checks out.
- `iris-checkout`, `iris-cherry-pick`, `iris-merge-to-master`: lease gate for moves that change what dogfood serves.
- `iris-dogfood-lease`: defines the host-mover gate.
- `iris-status`: host branch vs dogfood branch.

## Impact

`internal/verbs/{gh_pr_merge,checkout,cherry_pick,merge_to_master,complete_task,status}.go`, a new `host_move.go`, MCP handlers and tool catalog entries for `lease_override`, `claude/skills/iris/SKILL.md`, and removal of `.workflow/todo/gh-pr-merge-unlocked-post-merge-checkout.md`. `iris_reload` and `iris_set_dogfood` are untouched (owned by a sibling change).
