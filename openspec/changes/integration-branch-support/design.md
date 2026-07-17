## Context

iris's existing merge verb, `iris:merge_to_master` (`internal/verbs/merge_to_master.go`), operates directly on the source repo's live checkout: `fetch` → `checkout <default>` → `pull --ff-only` → `merge` → (push is a separate verb). That's safe there because the *target* is always the default branch, which tracks `origin`, and the verb is explicitly allowed to move the source repo's checkout — nothing else in iris depends on the source repo staying on whatever branch the human/agent left it on before the call.

A long-lived integration branch breaks both assumptions: the target is an arbitrary branch (not the default), and moving the source repo's checkout to merge into it would be an unacceptable side effect — the source repo is a single shared checkout (per `lockSourceRepo`), and another concurrent iris call, or the human, may be relying on its current branch staying put. Hence the scratch-worktree requirement: do the whole merge in a disposable `git worktree add <tmpdir> <target_branch>`, and never touch the source repo's own `HEAD`.

`iris:gh_pr_create` (`internal/verbs/gh_pr_create.go`) already distinguishes *which repo* a PR targets (`base_repo`) from auto-detected fork behavior, but conflates "which branch" with "the repo's default branch" in every mode. Adding `base` is a narrow, orthogonal parameter — repo selection and branch selection are independent axes.

## Goals / Non-Goals

**Goals:**
- Let an agent merge an arbitrary `source_ref` into an arbitrary long-lived `target_branch` and push, without disturbing the source repo's current checkout.
- Reuse `iris:merge_to_master`'s proven shape (lock, dry-run, deferred-abort-on-cancel, post_merge hook) so the new verb behaves predictably to anyone already familiar with the existing one.
- Let `iris:gh_pr_create` target an explicit branch, composing cleanly with all three existing repo-target modes.
- Preserve every existing safety rail on both verbs; add narrow new ones only where the broadened scope creates a real hazard.

**Non-Goals:**
- `iris:merge_to_branch` does not create `target_branch` if it doesn't exist anywhere (locally or on origin) — that's `iris:branch_create`'s job. It errors naturally when neither exists.
- It does not replace `iris:merge_to_master`; merging into the default/protected branch is explicitly refused and redirected to the existing verb, which enforces the `argus/`-prefix source restriction that `merge_to_branch` intentionally does not.
- No support for merge strategies beyond `--no-ff` / `--ff-only` (mirrors `iris:merge_to_master`'s existing `no_ff` knob — no new strategy surface).
- `base` on `iris:gh_pr_create` does not validate that the named branch exists on the target repo; `gh pr create` surfaces that failure itself, same as an invalid `head` today.

## Decisions

### Scratch worktree lifecycle owns fetch, checkout, and the "doesn't track origin" reset

`setupScratchWorktree(ctx, sourceRepo, targetBranch)` does, in order: `git fetch --all --prune` (so both `target_branch` and `source_ref` resolve against current remote state), `git worktree add <tmpdir> <target_branch>` (git's own DWIM creates a local tracking branch from `origin/<target_branch>` when no local ref exists — exactly the "create-on-first-use" behavior we want, with no bespoke logic), then conditionally `git reset --hard origin/<target_branch>` **only if `origin/<target_branch>` resolves**. That last step is what makes an out-of-date or non-tracking local ref safe to merge from: `merge_to_master`'s `pull --ff-only` assumes a tracking relationship and fails loudly without one; here we sidestep the assumption entirely by explicitly checking for the remote ref and hard-resetting the disposable worktree to it when found. If `origin/<target_branch>` doesn't exist yet (a brand-new branch never pushed), the worktree keeps whatever local state `worktree add` gave it and the eventual push creates the upstream branch.

The cleanup half (`git worktree remove --force`, falling back to `rm -rf` + `worktree prune` if that fails) runs via a returned closure the caller defers immediately, so it fires on every return path including panics-via-defer-chain, mirroring the existing deferred-abort pattern.

**Alternative considered**: reuse `mergeToMasterLocked` with a branch parameter instead of a hardcoded default. Rejected — that function operates on `resolved.SourceRepo` directly (the live checkout) by construction; retrofitting a worktree indirection into it would touch tested, shipped behavior for `iris:merge_to_master` merely to share a few lines with a verb that has fundamentally different disturb-the-checkout semantics.

### `git worktree add` refusing an already-checked-out branch is a feature, not a bug

If the source repo's own checkout (or another worktree) already has `target_branch` checked out, `git worktree add` fails outright ("already used by worktree at ..."). We don't work around this — it's git's own enforcement of exactly the invariant we want (never operate on a branch that's live elsewhere), so the verb just surfaces the wrapped error.

### New guard function, not `guardBranch`

`guardBranch` (merge_to_master.go:394-409) hardwires "source must be `argus/*`, target must be the default branch." Neither constraint applies here — `source_ref` is deliberately unscoped (feature branches, tags, SHAs, anything) and `target_branch` is deliberately non-default. The new `guardMergeToBranch(targetBranch, sourceRef, defaultBranch)` keeps only the guards that generalize:
- both args required (empty → error)
- neither may begin with `-` (the same flag-smuggling guard already used for `head`/`branch`/`remote`/`base_repo` elsewhere in the verb set)
- `target_branch == source_ref` is refused (a no-op merge into itself is never intended input, almost certainly a caller mistake)
- `target_branch` equal to the default branch (or the literal `main`/`master`, mirroring `guardBranch`'s belt-and-suspenders literal check) is refused, redirecting to `iris:merge_to_master` — this is the one guard added *because* of the broadened scope: without it, `merge_to_branch` would be a way to merge an unscoped `source_ref` into the default branch, silently bypassing `merge_to_master`'s `argus/`-prefix restriction on what may land on production.

### post_merge hook reads `.iris.toml` from the merged worktree, not the source repo

`iris:merge_to_master`'s `runPostMergeHook` reads `.iris.toml` from `resolved.SourceRepo` because that checkout ends up *on* the default branch post-merge. `iris:merge_to_branch` never moves the source repo's checkout, so reading `.iris.toml` from there would run whatever hook happens to be configured on a branch entirely unrelated to this merge. Instead the new `runMergeToBranchPostMergeHook` reads `.iris.toml` from the scratch worktree (post-merge, pre-cleanup) — the actual tree that now represents `target_branch`. Env vars are also renamed to match this verb's shape: `IRIS_TARGET_BRANCH` and `IRIS_SOURCE_REF` replace `IRIS_DEFAULT_BRANCH`/`IRIS_TASK_BRANCH`; `IRIS_TASK_ID`, `IRIS_SOURCE_REPO`, `IRIS_MERGE_SHA` carry over unchanged. This is a small, deliberate duplication of `runPostMergeHook`'s body rather than a shared abstraction — the config path and env vars genuinely differ, and threading that variance through one shared function would obscure both call sites for a one-time save.

### `iris:gh_pr_create`'s `base` composes as a branch override layered under all three repo-target modes

Each of the three existing branches (`base_repo` set → `--repo <base_repo>`, omit `--base`; fork auto-detected → `--repo <upstream>`, omit `--base`; same-repo → `--base <default>`) currently either omits `--base` (letting gh default it to the target repo's own default branch) or hardcodes the resolved default branch. `base`, when non-empty, is simply substituted in: appended as `--base <base>` in the first two modes (where it was previously omitted), and used in place of `defaultBranch` in the third. Precedence and repo-selection logic (`base_repo` > fork-detection > origin) are completely unchanged; `base` is an orthogonal axis, not a fourth mode. Validated with the same leading-dash guard as `head`/`base_repo` before gh ever runs.

## Risks / Trade-offs

- **Scratch worktree disk/perf cost**: every `merge_to_branch` call does a fresh `worktree add` + fetch. Acceptable — integration-branch merges are infrequent relative to per-task merges, and the worktree is removed immediately after.
- **`target_branch` racing a concurrent human checkout**: mitigated by the per-source-repo lock (`lockSourceRepo`) held for the full scratch-worktree lifecycle, same as every other mutating verb; a concurrent `git checkout <target_branch>` on the source repo between lock acquisition and `worktree add` is a pre-existing hazard class the lock already covers for all verbs, not new here.
- **Push race**: if `origin/<target_branch>` moves between our `reset --hard` and the final `push`, the push is rejected (non-fast-forward) and surfaces as an error; iris does not retry or force-push. The local (shared) `target_branch` ref will have advanced past the merge inside the source repo's ref table even though the push failed — this mirrors the existing, accepted trade-off in `iris:merge_to_master` (a successful local merge with a subsequent failed dependent step is reported as an error, not rolled back).

## Migration Plan

Additive only — new verb, new optional parameter. No migration needed.

## Open Questions

None.
