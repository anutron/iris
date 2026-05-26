## Why

After `merge_to_master`, `push`, and (optionally) `gh_pr_merge`, the agent still has to: push the default branch to origin, delete the remote task branch, mark the argus task complete, and archive it. That's four host-side ops in a row, every one of which fails differently. `iris:complete_task` runs the whole sequence under one verb with a checkpoint contract so a partial failure tells the caller exactly where to resume.

This is the verb that completes the argus task lifecycle. It's why every other iris verb exists: so an agent inside a worktree can ship its work end-to-end without typing "Aaron, can you run X?" once.

## What Changes

- Add `iris:complete_task` verb composing the existing `merge_to_master` plus three new host-side ops (default-branch push, remote-task-branch delete, argus task state transitions).
- Add `SetTaskStatus(ctx, id, status)` and `ArchiveTask(ctx, id)` to the argus client.
- Wire MCP handler `iris_complete_task` and CLI subcommand `iris complete-task <task-id>`.

## Capabilities

### New Capabilities

- `iris-complete-task`: the composite verb's input contract, checkpoint sequence, idempotency rules, and structured response.

### Modified Capabilities

None directly. The verb uses `merge_to_master` (in `iris-merge-to-master`) and adds host-side ops the existing capabilities don't cover.

## Impact

- No new host state, no installer change.
- Two new argus REST consumers: `POST /api/tasks/<id>/status` and `POST /api/tasks/<id>/archive`. Both already exist server-side per the handoff brief.
- Archive deletes the worktree; documented as best-effort with the task left in "complete" status if argus refuses to archive (operator cleans up manually).
