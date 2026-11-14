## Context

The host checkout of a dogfooded repo is shared state: its branch determines what the dogfood server builds. Verbs that run `git checkout` there change that silently.

## Decisions

### D1: gh_pr_merge never checks out

After the remote merge: `lockSourceRepo`, then `git fetch origin <default>` via `runGitTransfer` with `gitTransferTimeout`, then:

- host on the default branch → `git merge --ff-only origin/<default>` (no network); failure → warning.
- host on any other branch (dogfood, feature, detached) → no checkout. If `refs/heads/<default>` exists, is not checked out in any worktree, and is an ancestor of `origin/<default>`, move it with `git update-ref <ref> <new> <old>` (old-value guarded). Diverged, worktree-held, or missing → warning (missing is silent).

`update-ref` is safe here because the default branch is not checked out anywhere (a checked-out ref would leave HEAD/index inconsistent; that case is skipped with a warning).

Rejected: checkout-then-restore (still reloads the server mid-way); skip syncing entirely (leaves `iris_reload` stale).

### D2: lease gate for deliberate host movers

A shared helper `hostMoveGate` decides whether a move "changes what dogfood serves": the host currently sits on `dogfood_branch` and the target differs, or the verb modifies `dogfood_branch` itself (cherry-pick onto it). If so and another task holds the lease (read via `leaseHolderRef`), the verb refuses with a `LeaseGateError`. `lease_override=true` proceeds, puts a `LEASE OVERRIDE:` warning first in the result, and notifies the holder (`noticeHolder`, `leaseMsgOverride`). A free lease, or a lease held by the caller, passes. Repos with no `dogfood_branch` are unaffected. The gate runs before `lockSourceRepo`, matching `iris_set_dogfood`'s ordering.

`iris_complete_task` runs the same gate without an override input; a blocked complete reports the refusal and the agent resolves the lease first.

### D3: audit of siblings

- `iris_tag`: creates a tag and pushes; no checkout. No change.
- `iris_ship_feature`: pushes a branch ref, recomposes in a scratch worktree, deploys through `SetDogfood`; no direct host checkout. No change.
- `iris_merge_to_branch`: scratch worktree; documented as host-safe.
- `iris_merge_to_master` / `iris_complete_task`: need the default branch checked out for `merge --ff-only`; gated by D2.

### D4: status

`iris_status` gains `dogfood_branch` and `host_off_dogfood` (true when `dogfood_branch` is set, the host branch is known, and they differ) and a warning naming both.
