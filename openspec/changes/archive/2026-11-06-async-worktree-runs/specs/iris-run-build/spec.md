## ADDED Requirements

### Requirement: `iris:run_build` runs as a worktree run

The `iris_run_build` MCP tool SHALL follow the worktree-runs contract: it runs detached from the caller under the iris-owned ceiling, accepts `wait_seconds`, returns its existing response when the build finishes within the wait, and otherwise returns a `run_id` handle that `iris_run_result` resolves. The deduplication key SHALL be the `task_id` plus `target`.

#### Scenario: Long build returns a handle

- **WHEN** `iris_run_build` is still compiling after `wait_seconds`
- **THEN** the tool returns `status: "running"` with a `run_id`, and `iris_run_result(run_id)` later returns `{command, exit_code, output}`
