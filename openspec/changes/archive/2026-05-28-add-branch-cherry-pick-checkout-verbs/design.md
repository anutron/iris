# Design: branch/cherry-pick/checkout verbs

## Context

A worker in an argus sandbox can produce a commit on its `argus/...` worktree branch but cannot, today, take that commit and put it on a fresh branch off `origin/master` from inside the sandbox — the `git` writes against the source repo are blocked. The composite flow needs three steps that iris doesn't currently expose:

1. Create a branch off `origin/master` in the source repo.
2. Cherry-pick the worktree's commit onto that branch.
3. (Recover if any of step 1 or 2 leaves the source repo in a half-done state.)

These three new verbs cover that flow with the same shape as v1.1's git verbs.

## Goals

- Make the cherry-pick-from-worktree-commit flow expressible end-to-end through iris.
- Add a real "get unstuck" verb so half-failed merges/cherry-picks don't require host intervention.
- Reuse v1.1 patterns: typed inputs, per-source-repo lock, structured results, flag-smuggling guards, real `git` against tempdirs for tests.

## Non-goals

- No "branch_create-and-checkout" composite; the caller composes `branch_create` + `checkout` if they want both.
- No `cherry_pick_to_master` shortcut; cherry-picking onto the default branch is refused (use `merge_to_master` instead).
- No `iris:branch_delete_local`; the workflow has no need yet, and `git branch -D` is safer to leave off the surface.
- No `iris:reset` (still destructive, still no current workflow).

## Decisions

### D1. `iris:branch_create` does NOT check out

`branch_create` runs `git branch <name> <base_ref>` (creates the ref, leaves HEAD where it is). Checkout is a separate verb. This avoids surprising the source repo's checkout state for callers who only want the ref. Composing `branch_create` then `checkout` covers the "create + switch" case.

### D2. `iris:branch_create` accepts arbitrary base refs

`base_ref` can be `origin/master`, a SHA, `refs/remotes/origin/main`, a tag — anything `git rev-parse` resolves. iris does not pre-fetch; the caller is expected to have called `iris:fetch` first if they want the latest origin state. Pre-fetching from inside `branch_create` would couple two operations and surprise a caller that wanted to branch off a known-stale ref.

### D3. `iris:branch_create` refuses the default branch name

Refuses if `name` equals the resolved default branch, or `main`, or `master`. Matches the existing default-branch-protection invariant from `iris:push` and `iris:merge_to_master`. The verb is for creating non-default branches; clobbering `main` via this verb would be a foot-gun even if origin/HEAD says something else.

### D4. `iris:branch_create` refuses existing branches

`git rev-parse --verify refs/heads/<name>` is the pre-check. Refuses if the branch exists locally. No `force=true` knob — the workflow has no need for it, and adding force here is the kind of foot-gun the recovery verb (`iris:checkout`) is meant to absorb explicitly elsewhere.

### D5. `iris:cherry_pick` checks out the target branch first

`cherry_pick` calls `git checkout <target_branch>` then `git cherry-pick <commit>`. Both happen under one lock so the checkout-and-pick pair is atomic from another iris caller's perspective. The verb leaves the source repo on `target_branch` after success.

### D6. `iris:cherry_pick` refuses the default branch as target

The use case is hotfix branches and PR branches, not master. Cherry-picking onto master conflicts with `iris:merge_to_master`'s contract (the canonical "land on master" verb) and would let agents bypass the merge-from-argus-branch invariant. Refusal here keeps the model coherent: master is reached by `merge_to_master`, never by `cherry_pick`.

### D7. `iris:cherry_pick` aborts cleanly on conflict

If `git cherry-pick` returns non-zero, iris runs `git cherry-pick --abort` (best-effort) and returns a structured error. The source repo lands back on `target_branch` with a clean working tree, mirroring the abort-on-conflict semantics already in `merge_to_master`. The caller gets `conflicts` (paths from `git diff --name-only --diff-filter=U`) so they can decide whether to retry differently.

### D8. `iris:checkout` is a real recovery verb in `force=true` mode

In default mode, `iris:checkout <branch>` is plain `git checkout <branch>` — git refuses if the working tree is dirty or a merge/cherry-pick is in progress. With `force=true`, iris runs the full recovery sequence first:

1. `git merge --abort` (best-effort, ignored if no merge in progress)
2. `git cherry-pick --abort` (best-effort)
3. `git rebase --abort` (best-effort)
4. `git checkout -f <branch>` (discards uncommitted changes and switches)

This is the explicit "I know this is destructive; get me unstuck" path. Without `force=true`, the destructive bits never run. The verb returns the prior branch and HEAD SHA so the caller can see what state was discarded if they need to recover from a misfire.

### D9. Branch-name and ref validation

All three verbs reject any branch-name or commitish that starts with `-` (argv flag-smuggling guard, matching `iris:branch_delete_remote`). All three reject empty values. `branch_create` additionally calls `git check-ref-format --branch <name>` to reject names git itself would reject (avoids creating refs that then can't be deleted via the normal verb).

### D10. Lock scope is the full operation

Each verb holds the per-source-repo lock from after `verbs.Resolve` returns until the last git process exits. For `cherry_pick`, that covers both the checkout and the pick. Standard v1.1 pattern.

## Acceptance criteria

### `iris:branch_create`

- it should refuse when `name` or `base_ref` is empty, naming the field
- it should refuse when `name` or `base_ref` starts with `-` (flag-smuggling guard)
- it should refuse when `name` equals the resolved default branch, or equals `main`, or equals `master`
- it should refuse when `name` is not a valid git ref (`git check-ref-format --branch` fails)
- it should refuse when the branch already exists locally, naming the conflict
- it should refuse when `base_ref` does not resolve, carrying git's error
- it should create the branch via `git branch <name> <base_ref>` and return `{ created: true, branch, base_ref, sha }`
- it should NOT change the source repo's current checkout
- it should hold the per-source-repo lock for the duration of the operation
- it should mirror the same Go function from CLI invocation (`iris branch-create <task-id> <name> <base-ref>`)

### `iris:cherry_pick`

- it should refuse when `commit` or `target_branch` is empty, naming the field
- it should refuse when `commit` or `target_branch` starts with `-` (flag-smuggling guard)
- it should refuse when `target_branch` equals the resolved default branch (or `main`/`master`), naming both
- it should refuse when `target_branch` does not exist locally, carrying git's error
- it should refuse when `commit` does not resolve, carrying git's error
- it should checkout `target_branch`, run `git cherry-pick <commit>`, and return `{ cherry_picked: true, commit, target_branch, new_sha }` on success
- it should run `git cherry-pick --abort` on conflict, return a structured error carrying conflict paths, and leave the source repo on `target_branch` with a clean working tree
- it should hold the per-source-repo lock for the duration of the checkout + cherry-pick pair
- it should mirror the same Go function from CLI invocation (`iris cherry-pick <task-id> <commit> <target-branch>`)

### `iris:checkout`

- it should refuse when `branch` is empty
- it should refuse when `branch` starts with `-` (flag-smuggling guard)
- it should refuse when the branch does not exist locally and there is no matching `origin/<branch>` (carrying git's error)
- it should checkout `branch` via plain `git checkout <branch>` when `force=false` and return `{ checked_out: true, branch, head_sha, prior_branch, prior_head }`
- it should refuse with git's error when `force=false` and the working tree is dirty or a merge/cherry-pick is in progress
- it should run `git merge --abort`, `git cherry-pick --abort`, `git rebase --abort` (each best-effort) then `git checkout -f <branch>` when `force=true`, discarding uncommitted changes
- it should hold the per-source-repo lock for the duration of the operation
- it should mirror the same Go function from CLI invocation (`iris checkout <task-id> <branch> [--force]`)

## Risks / Trade-offs

- **Risk:** `iris:checkout --force` discards work an agent didn't intend to lose. → Mitigation: result includes `prior_branch` and `prior_head` so the SHA isn't unrecoverable (still reachable via reflog). `force` is opt-in.
- **Risk:** Cherry-picking onto a branch that's behind origin causes a divergent local branch the next push can't fast-forward. → Mitigation: out of scope here; the caller is expected to fetch first (D2). Documented as part of the cherry-pick flow rather than enforced by this verb.
- **Trade-off:** `cherry_pick` couples checkout + pick under one lock. Costs slightly longer hold time vs. separate verbs, but the atomicity is what makes the verb safe to call without external coordination.

## Migration plan

1. Implement on `argus/iris-yeah-right-framing`; ship as the v1.2 (or next) batch.
2. Dogfood the new verbs by using `branch_create` + `cherry_pick` + `gh_pr_create` to open the PR that lands this change.
3. After dogfood, archive the change.
4. Rollback: revert. No state to clean up.

## Open questions

None.
