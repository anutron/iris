# gh_pr_merge runs its post-merge checkout without the source-repo lock

- **Found:** 2026-10-19, during the `name-resolved-repo` change (sketch-guides-docs retro follow-up).
- **Where:** `internal/verbs/gh_pr_merge.go`, after `gh pr merge` succeeds.
- **What:** `GHPRMerge` checks out the default branch and runs `git pull --ff-only` in the canonical source repo, but never takes `lockSourceRepo`. Every other verb that mutates the source repo's checkout (`gh_pr_close`, `checkout`, `merge_to_master`, `push`, …) holds that lock.
- **Risk:** a concurrent `merge_to_master`, `checkout` or `cherry_pick` on the same source repo can interleave with the checkout and pull. One of them could fail on an unexpected branch, or the pull could land mid-merge.
- **Likely fix:** hold `lockSourceRepo(resolved.SourceRepo)` around the post-merge checkout and pull (the `gh` call itself needn't be inside it), and add a serialization test like `TestGHPRClose_ConcurrentCallsSerialize`.
- **Status:** not fixed. The coordinator asked for a note only.
