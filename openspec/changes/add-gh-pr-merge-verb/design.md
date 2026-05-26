## Context

`iris:gh_pr_merge` is the fourth iris verb. It pairs with `iris:gh_pr_create` to complete the PR lifecycle: open, then merge. Like its sibling, it's a thin wrapper over the host's gh CLI – iris stays out of the GitHub API and lets gh handle auth, host resolution, and merge mechanics.

## Goals / Non-Goals

**Goals:**

- One subprocess call to `gh pr merge <num> --<strategy>` with an explicit strategy enum the verb owns.
- Strategy validation lives at the verb boundary and is RE-checked in the MCP handler (defense in depth).
- Surface gh's stderr in the verb error so common failures (CI red, conflicts, merge already happened, no auth) are diagnosable from the verb error alone.

**Non-Goals:**

- Pre-checking CI status before merging. gh's own merge already fails if required checks are red. Adding `gh pr checks <N>` as a pre-step is non-breaking but doubles the network calls for negligible gain in v1.
- Auto-merge enablement (`--auto`). That's a different verb (`iris:gh_pr_enable_auto_merge`) if we ever need it; folding it into a flag here muddies the contract.
- Holding the per-source-repo mutex. Like `gh_pr_create`, this is a network op against GitHub, not a local source-repo mutation. Two concurrent merges of distinct PRs are independent.
- Resolving `pr_number` from the branch. The agent that just created the PR has the number in hand; making this verb look it up adds latency and a new failure mode for no benefit.

## Decisions

### Strategy is a closed enum

squash|merge|rebase are the three gh-supported strategies. Passing an unknown strategy means the caller has a bug; we reject it at the verb boundary BEFORE invoking gh so the bug is named immediately rather than wrapped in a gh error string. `IsValidGHPRMergeStrategy` is exported so the MCP handler can validate without duplicating the set.

### Default strategy is squash

squash matches how most agent-generated commits should land: many small commits squashed to one. Merge and rebase are the explicit overrides. Default lives in the MCP handler (and the CLI), not the verb itself — the verb requires the caller to pass a valid strategy. This means the verb is harder to call accidentally and easier to test.

### gh runs in the source repo, not the worktree

Same reasoning as `iris:gh_pr_create`: gh resolves the remote from the source repo's git config; running in the worktree adds nothing and constrains the verb to a clean worktree state.

### v1 does not gate on CI

The brief explicitly defers CI gating to the caller. gh returns non-zero when required checks are red — that surface is sufficient for v1. The PR is open about this and documents the trade-off.

### `pr_number` validation at the verb boundary

`pr_number <= 0` is rejected before the subprocess is touched. The handler ALSO validates this (`pr_number must be a positive integer`) so an MCP caller sees the same error shape regardless of which layer rejects it.

## Risks / Trade-offs

- **[Trade-off] No CI pre-check.** If gh's merge changes its behavior on red CI (e.g., gh learns auto-wait), the verb's failure mode shifts. Mitigation: surface gh stderr verbatim so the operator can read what happened.
- **[Risk] Strategy validation drift.** If gh adds a new strategy (unlikely but possible), our enum will reject it. Mitigation: enum is a single line; update is trivial.
- **[Trade-off] No mutex.** Same as `gh_pr_create` — overlapping calls for the same PR will get `gh: PR already merged` on the second, which surfaces as an error and is the right behavior.
- **[Risk] `pr_number` mismatch with `task_id`.** Iris does not cross-check that the PR's head branch matches the task's branch. A caller could merge an unrelated PR. Mitigation: caller orchestrates; this is acceptable because the alternative (a remote round-trip to verify) doubles latency for a class of bug that's already self-evident in the caller's logs.

## Migration Plan

- Verb-only addition. No installer changes, no host state, no schema migration.
- Rollback: revert the commit; the daemon stops registering `iris_gh_pr_merge` and the tool disappears from argus's allowlist on next heartbeat.

## Open Questions

- Should we add `gh pr checks <N>` as a pre-step? Reasonable follow-up. Easy to add as opt-in `wait_for_ci: bool`.
- Should the verb cross-check that the PR's head branch matches the task's resolved branch? Trade-off above; defer until a real misuse case appears.
