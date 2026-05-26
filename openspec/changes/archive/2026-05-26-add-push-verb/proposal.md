## Why

Agents inside argus worktrees cannot push their task branch to origin: the sandbox blocks network ops against the canonical remote, and even when it allowed them, the agent has no credentials. `iris:push` is the host-side verb that closes the loop after `merge_to_master` or any in-task work: it pushes the resolved task branch to origin from the canonical source repo, reusing the host's existing `~/.ssh` and `~/.gitconfig`.

## What Changes

- Add `iris:push` verb. Resolves source repo from `task_id`, refuses to push the default branch, holds the per-source-repo mutex, runs `git push origin <branch>` with optional `--force-with-lease`.
- Wire MCP handler `iris_push` and CLI subcommand `iris push <task-id>`.

## Capabilities

### New Capabilities

- `iris-push`: The `iris:push` verb specifically — input contract, default-branch refusal, push command, structured response with the remote SHA.

### Modified Capabilities

None.

## Impact

- No new host state, no installer change.
- gh and ssh credentials reused as-is.
- Push is narrow: only the task branch goes up. Tag pushes and other-branch pushes are deliberately out of scope.
