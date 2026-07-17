## Context

`SetDogfood` runs under the per-source-repo lock: read `previous_sha` → write manifest (durable-first) → move the dogfood ref → release lock → reload with `BuildBranch = dogfood`. Both fixes slot into the locked window; neither changes the lock discipline, the durable-first ordering, or the "iris is dumb" contract.

## Decision 1: worktree-guarded hybrid ref move

`git branch -f` refuses to move a branch checked out in any worktree. That is the exact failure on a live dogfood host, where the dogfood branch is the checked-out branch.

Options considered:

- **`git update-ref refs/heads/<dogfood> <newSHA>`** — moves the ref regardless of checkout state. REJECTED: it moves the ref out from under the checked-out worktree, leaving HEAD/index pointing at the old tree while the ref points at the new one — the working copy shows phantom staged diffs and the next status is wrong. Explicitly ruled out with the user.
- **Always `git reset --hard`** — REJECTED: for the common case (dogfood is a bare ref, not checked out) it would need a temporary checkout and would mutate a working tree unnecessarily, breaking the current ref-only contract.
- **Guarded hybrid (chosen)** — keep `git branch -f` for the common (not-checked-out) case; use `git reset --hard <newSHA>` in the worktree that has the branch checked out otherwise. Both land the ref at `newSHA`; the reset also updates that worktree's HEAD/index/tree consistently, so no phantom diffs.

Detection: parse `git worktree list --porcelain` in the source repo. Each entry is a `worktree <path>` line, a `HEAD <sha>` line, and either a `branch refs/heads/<name>` line or a `detached` line, separated by blank lines. The dogfood branch is checked out iff some entry has `branch refs/heads/<dogfood>`; that entry's `<path>` is where the `reset --hard` runs.

This only applies to the force-move path (branch already exists). The create-when-absent path (`previous_sha == ""`) is untouched — a branch that does not exist cannot be checked out.

### Interaction with reload

`SetDogfood` calls `reload` with `BuildBranch = <dogfood>` right after the ref move. Reload checks out the dogfood branch to build the composed SHA and restores the entry branch afterward (`reload.go:236`, `reload.go:228,400`). The reset-hard branch of this fix is deliberately confined to the ref move; reload's checkout/restore behavior is unchanged. Reload's own pre-flight (source repo must be on its default branch) is also unchanged.

## Decision 2: ancestry-check + refuse/warn (keep iris dumb)

The danger: a `newSHA` that does not contain `previous_sha` drops every commit reachable from `previous_sha` but not from `newSHA`.

- Check: `git merge-base --is-ancestor <previous_sha> <newSHA>`. Exit 0 → `previous_sha` is an ancestor of `newSHA` (safe: no commits dropped, includes the no-op `newSHA == previous_sha` case). Non-zero → not an ancestor (diverged or strictly behind) → commits would be dropped.
- On a drop: count with `git rev-list --count <newSHA>..<previous_sha>` and REFUSE with an error naming the count and `previous_sha`, unless `force` is set.
- With `force`: proceed and append a prominent `warnings` entry naming the count and `previous_sha`.
- Placement: immediately after reading `previous_sha`, BEFORE the manifest write, so a refusal leaves no side effects (matches the existing "refuse before any mutation" scenarios). Skipped entirely when `previous_sha == ""` (branch being created — nothing to drop).

Iris does not compose. The refusal text points the agent at the remedy (recompose on the current dogfood SHA, or pass `force`); iris never merges or cherry-picks on the agent's behalf.

## `force` input

Optional boolean, default `false`, threaded through the four surfaces (`SetDogfoodOpts`, MCP handler, CLI `--force`, tool schema). It ONLY relaxes the ancestry refusal; it does not affect config resolution, SHA reachability, the worktree guard, or reload.

## Test strategy

- Worktree-guard: create the dogfood branch, check it out in a worktree, and assert the ref move lands the branch at the composed SHA (the old `git branch -f` left it unmoved and errored). This replaces the prior "branch reset failure leaves manifest ahead" test, whose only trigger (a checked-out branch) is exactly the case this fix now handles. Durable-first ordering stays covered by the manifest-write-failure test.
- Ancestry: a `newSHA` that diverges from `previous_sha` is refused (error names the dropped-commit count) without `force`, and proceeds (emitting a warning) with `force`. A descendant `newSHA` proceeds normally.
- Regression guards: overlay-only `dogfood_branch` still resolves; the composed-SHA build still deploys the dogfood tree.
