## Why

Feedback from an argus agent consuming iris during a real session surfaced six rough edges. Two cost real steps every session (status doesn't surface enough context to action; post-merge cleanup is three manual calls). Four are polish that compounds across sessions (missing postconditions, noisy warning, no dry-run safety, undocumented relationship between merge and worktree cleanup).

The unifying theme: iris exposes the right primitives but doesn't help consumers chain them. Consumers (humans, argus agents, future automation) end up reinventing the same recipes – which couples them to iris's surface even when iris could collapse the coupling itself.

This change closes the gap without baking consumer-specific recipes into iris. The post-merge hook is configured per-repo in `.iris.toml`, not in iris's code. The status enrichment exposes data iris already has internally. Postconditions describe iris's own output, not the consumer's next action.

## What Changes

### `iris:status`

- Add `branch` (current HEAD branch name) to the structured result.
- Add `argus_task` (the resolved `argus.Task` when iris can find one whose `worktree_path` matches the resolved source repo) – populated only when a match exists; null otherwise.
- Treat a missing `.iris.toml` as a non-event: return `config: null` with NO warning. Parse errors still surface as warnings.

### `iris:merge_to_master`

- Add factual postconditions to the success result: `task_branch_still_exists` (bool) and `worktree_still_present` (bool). These describe iris's output state, not the consumer's next action.
- Add `dry_run` input (bool, default false). When true, iris runs `git merge --no-commit --no-ff <branch>`, captures the would-be merge state (files changed, conflicts), aborts cleanly, and returns a preview-shaped result with `dry_run: true`. No commit, no post_merge hook.
- Run an optional `[post_merge]` hook from `.iris.toml` after a successful (non-dry-run) merge. The hook is a `command` argv with optional `working_directory` and `timeout_seconds`. iris exports `IRIS_TASK_ID`, `IRIS_TASK_BRANCH`, `IRIS_SOURCE_REPO`, `IRIS_DEFAULT_BRANCH`, `IRIS_MERGE_SHA` to the command's environment. Stdout/stderr/exit code/duration are captured into `post_merge` on the result.
- Update the MCP tool description to clarify that the verb does NOT delete the task branch or the worktree; reference `iris_complete_task` and `iris_branch_delete_remote` as follow-ups.

### `.iris.toml`

- New optional `[post_merge]` block with the same shape as `[pre_flight]` and `[verify]` (a `HookBlock`).
- Schema is additive; no version bump.

## Capabilities

### Modified Capabilities

- `iris-status`: add `branch` and `argus_task` to the result; downgrade "missing .iris.toml" from a warning to a silent null config.
- `iris-merge-to-master`: add `dry_run` input, `task_branch_still_exists` / `worktree_still_present` / `post_merge` to the result, and `[post_merge]` hook execution.

### New Capabilities

- (none) – the post_merge hook lives inside the existing `iris-merge-to-master` capability rather than as a separate verb.

## Impact

- `internal/verbs/status.go` + tests: enrich result, silence missing-config warning.
- `internal/argus/tasks.go`: add `ListTasks` (or equivalent) so iris can reverse-lookup a task from a source-repo path.
- `internal/verbs/merge_to_master.go` + tests: dry-run flag, postconditions, post_merge hook execution, env export.
- `internal/config/iris_toml.go` + tests: optional `[post_merge]` field on `IrisToml`; treat ENOENT as "file absent" (return `(nil, nil, nil)`) instead of synthesizing a validation warning.
- `internal/mcp/handler_merge_to_master.go` + `handler_status.go`: pass through new input/output shape.
- `internal/daemon/run.go`: update tool descriptions and input schemas.
- `cmd/iris/merge_to_master.go`: add `--dry-run` CLI flag.
- README documents the new fields, the `[post_merge]` block, and the `--dry-run` flag.
