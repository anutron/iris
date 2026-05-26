## Context

`iris:push` is the second iris verb, following `iris:merge_to_master`. It exists for the same reason as merge_to_master: argus's sandbox blocks network and credential access to the canonical remote, so the agent inside the worktree can't push. The host-side iris daemon can.

The verb is narrow on purpose. `merge_to_master` does the high-trust git mutation; `push` does the high-trust network op. Keeping them separate means each verb has one obvious failure mode.

## Goals / Non-Goals

**Goals:**

- Push the task's branch to origin from the source repo (not the worktree — the worktree's branch is already a ref in the source repo via `git worktree`).
- Refuse to push the default branch. Pushing master/main from an agent is never the right verb to use; an explicit `iris:gh_pr_merge` handles default-branch updates via PR merge.
- Reuse the per-source-repo mutex so push serializes with `merge_to_master` against the same source repo (the post-merge push is the dominant ordering).

**Non-Goals:**

- Pushing tags. If a future verb needs to push a release tag, that's `iris:push_tag`, not a flag here.
- Pushing other branches. Iris resolves the branch from `task_id`; no agent-supplied branch input.

## Decisions

### Push runs in the source repo, not the worktree

`git worktree` makes the worktree's branch a ref in the source repo's refs/heads, so `git -C <sourceRepo> push origin <branch>` is the canonical form. Running in the worktree would work but require an extra `git -C` resolution.

### `--force-with-lease` is opt-in, never `--force`

Force-with-lease checks that the upstream is what we expect before overwriting, so it's safe-by-default for amend/rebase workflows. Plain `--force` is never exposed; if a caller needs it, that's a manual operator action.

### Refuses default branch, period

`iris:gh_pr_merge` is the verb for landing changes on the default branch. `iris:push` exists for the in-progress task branch only. This keeps the safety model tight.

### Shares the per-source-repo mutex with `merge_to_master`

The dominant pattern is merge-then-push. Holding the same mutex across both means an agent can call them back-to-back without a race against another task on the same source repo. The mutex is held only for the verb's duration, not across calls.

## Risks / Trade-offs

- **[Risk] Non-fast-forward push surprises caller.** Without `--force-with-lease`, a divergent remote rejects the push → Mitigation: surface git's stderr in the verb error so the caller knows to either rebase locally (inside the worktree) or call again with `force_with_lease=true`.
- **[Trade-off] No tag-push verb.** Adding it later as `iris:push_tag` is non-breaking. The narrow surface is the point.
- **[Trade-off] `--force` is unreachable.** If history surgery is needed, the operator does it manually. Iris will not provide that hammer to an agent.

## Migration Plan

- Verb-only addition. No installer changes, no host state, no schema migration.
- Rollback: revert the commit; the daemon stops registering `iris_push` and the tool disappears from argus's allowlist on next heartbeat.

## Open Questions

None — narrow surface, the contract is settled.
