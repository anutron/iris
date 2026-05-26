## Context

`iris:complete_task` is the composite ship-it verb. Five sub-steps in a fixed order; each is one host-side action; each can fail; the caller (a sandboxed agent) needs to know how far we got so a retry can resume.

The dominant invocation pattern: an agent finishes its work, runs tests inside the worktree, then calls `iris:complete_task` to land the change on master and clean up. Anything that fails between sub-steps requires human attention, but the structured checkpoint response means the human knows whether they need to undo a partial merge or just re-run the verb.

## Goals / Non-Goals

**Goals:**

- One verb call ships a task end-to-end on the happy path.
- Partial failure returns the checkpoints reached so re-invocation can resume safely.
- Already-complete tasks return success immediately (idempotent at the verb boundary).
- The default-branch push and remote-branch-delete steps reuse the per-source-repo mutex from `lockSourceRepo` so concurrent `complete_task` and `merge_to_master` calls on the same source repo serialize cleanly.

**Non-Goals:**

- CI gating before merge. `iris:gh_pr_merge` handles PR-mediated landing; `complete_task` is the direct-merge path for trusted, post-review work.
- Streaming progress to the caller. v1 returns the full checkpoint list at the end; SSE is a future verb-runtime concern.
- Server-side worktree GC. Argus owns the archive endpoint; iris just calls it.

## Decisions

### Sub-step ordering: merge → push default → delete remote task branch → mark complete → archive

This order is the safe one:

1. **Merge first** because everything else assumes the merge happened. If the merge fails (conflict), we want to stop before touching origin or argus state.
2. **Push default branch second.** Now origin's default branch reflects the merge.
3. **Delete remote task branch third**, while the task is still "in_progress" on argus, so a failed argus state update doesn't leave a dangling remote branch.
4. **Mark complete on argus fourth.** At this point the git side is final.
5. **Archive last** because argus's archive deletes the worktree, and we're still running git ops against it earlier. Archive is best-effort: if it fails the task is still "complete" on argus and the operator manually cleans up.

### Checkpoint contract: list of names, returned in result

Each completed step appends a checkpoint constant (`merged`, `default_branch_pushed`, `remote_task_branch_deleted`, `task_marked_complete`, `task_archived`). A partial failure returns the checkpoints reached plus a wrapped error. Already-complete tasks return all five checkpoints.

### Already-complete shortcut at the front

`GetTask` returns the current status. If it's already "complete", return success with all checkpoints (no work performed). This is the "agent didn't realise the verb already ran" recovery path — common enough that we shortcut explicitly.

### Default-branch push runs inline (not via `iris:push`)

`iris:push` refuses to push the default branch. That's the right rule for the standalone verb. `complete_task`'s job is specifically to push the default branch after merging into it, so the default-branch-push step runs inline with its own `git push origin <default>` call under the held mutex.

### Single lock acquisition across all three git sub-steps

The verb acquires `lockSourceRepo(resolved.SourceRepo)` once, then runs `mergeToMasterLocked` → `pushDefaultBranchLocked` → `deleteRemoteBranchLocked` without releasing. The lock is released before the argus status/archive calls because those don't touch the source repo and would unnecessarily extend the lock window across network round-trips.

`mergeToMasterLocked` exists specifically so `complete_task` can hold the lock continuously — the standalone `MergeToMaster` acquires and releases its own lock, then delegates to `mergeToMasterLocked` to do the actual work. This avoids the recursive-lock problem that `sync.Mutex` doesn't support.

### Remote branch delete: "already gone" is success

A retried `complete_task` after the remote branch was deleted on the first attempt should not fail. `branchAlreadyDeleted` matches git's two error spellings ("remote ref does not exist", "unable to delete... not found") and treats them as success.

### Archive failure is non-fatal

Once argus has the task marked complete, the verb's contract is satisfied. Archive deletes the worktree — a useful cleanup but not load-bearing for the ship-it semantics. A failed archive surfaces in `result.Error` but does not produce a Go error, so the caller sees the success path.

## Risks / Trade-offs

- **[Risk] Mid-flight `iris:complete_task` interrupted.** The host process could be killed between checkpoints. → Mitigation: each sub-step is idempotent. Re-invocation reads argus status; already-complete shortcut catches the common case; merge is the failure-prone step on re-invocation (already-merged branches fail `fetch + merge`).
- **[Risk] Merge re-invocation fails when already merged.** A retry after the merge step succeeded but the next step failed will hit "already up to date" or similar on the second merge. Tested behaviour in `TestCompleteTask_ResumeAfterPartialSucceeds`. → Mitigation deferred: argus task status is the safety net (an operator runs `argus task complete <id>` to manually mark and archive after a partial run; the merge has already landed).
- **[Trade-off] No SSE/streaming.** v1 returns the full checkpoint list at the end. Long-running merges (rare on text-only worktrees) appear opaque to the caller. Future verb-runtime work would address this for all verbs at once, not just complete_task.

## Migration Plan

- Verb addition only. No installer changes, no host state, no schema migration.
- Rollback: revert the commit; daemon stops registering the tool and it disappears from argus's allowlist on next heartbeat.

## Open Questions

- **Merge-step idempotency on retry.** A retry after the first attempt's merge already landed is effectively a no-op: `git merge --no-ff <branch>` resolves to "Already up to date." and exits zero, so the verb completes all five checkpoints on the second call. This works because the task branch is unchanged between attempts. If the operator manually rewinds the default branch between attempts, the retry would re-merge — also fine. No additional skip-merge-when-no-op logic is required.
