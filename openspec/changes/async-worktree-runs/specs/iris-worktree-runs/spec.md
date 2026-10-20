## ADDED Requirements

### Requirement: Worktree runs outlive the caller

`iris:run_checks` and `iris:run_build` SHALL run their script detached from the caller's cancellation, under an iris-owned ceiling of 30 minutes. They SHALL bound waiting on output pipes after the script exits or is killed, so an orphaned grandchild process can't hold the call or the per-worktree lock open. A run that exceeds the ceiling SHALL fail with an error containing `[timeout]` and the captured output.

#### Scenario: Caller cancellation does not kill the run

- **WHEN** the caller's context is cancelled while `script/iris-check` is running
- **THEN** the script continues to completion and its result is recorded

#### Scenario: Orphaned grandchild does not hold the run open

- **WHEN** the script exits while a background child it started keeps the output pipe open
- **THEN** the run finishes within a few seconds of the script's exit instead of waiting for the grandchild

### Requirement: Bounded wait with a run handle

`iris_run_checks` and `iris_run_build` SHALL accept an optional integer `wait_seconds`, defaulting to 20 and clamped to the range 0–25. The tool SHALL start the run and wait up to `wait_seconds` for it. If the run finishes in time, the tool SHALL return the same response it returned before this change. Otherwise it SHALL return a non-error JSON response containing `run_id`, `status: "running"`, `tool`, `task_id`, `started_at` and an instruction to call `iris_run_result`.

#### Scenario: Fast run answers inline

- **WHEN** `iris_run_checks(check="lint")` finishes in 2 seconds
- **THEN** the response is `{command, exit_code, output}` exactly as before, with no `run_id`

#### Scenario: Slow run returns a handle instead of timing out

- **WHEN** `iris_run_checks(check="test")` is still running after `wait_seconds`
- **THEN** the tool returns `status: "running"` with a `run_id` before the 30-second transport limit, and the run continues

#### Scenario: wait_seconds above the cap is clamped

- **WHEN** the caller passes `wait_seconds=120`
- **THEN** the tool waits at most 25 seconds before returning a handle

### Requirement: `iris:run_result` verb

The plugin SHALL expose `iris_run_result` accepting `run_id` (string, required) and `wait_seconds` (integer, optional, default 20, clamped to 0–25). For a finished run, it SHALL return the response the original tool would have returned synchronously, including the error response for a non-zero exit. For a run still in progress after the wait, it SHALL return the `running` handle again. For an unknown or expired `run_id`, it SHALL return an error saying the run is unknown, that iris may have restarted or the run expired, and that the caller should re-run.

#### Scenario: Poll returns the finished result

- **WHEN** a run returned as `running` later exits 1 with test failures, and the caller calls `iris_run_result(run_id)`
- **THEN** the response is the error response naming the command, exit code 1 and the full output

#### Scenario: Poll before completion returns the handle

- **WHEN** the caller polls a run that is still running after `wait_seconds`
- **THEN** the response is `status: "running"` with the same `run_id`

#### Scenario: Unknown run_id

- **WHEN** the caller polls a `run_id` iris has never issued, or one evicted after a restart
- **THEN** iris returns an error naming the `run_id` and advising a re-run

### Requirement: Duplicate runs share a handle and finished runs expire

While a run is in progress, a call to the same tool with the same `task_id` and the same `check` or `target` SHALL join that run and return its `run_id` instead of starting another. Finished runs SHALL remain retrievable for one hour after they finish and SHALL then be discarded.

#### Scenario: Retry joins the in-progress run

- **WHEN** `iris_run_checks(task_id=T, check="test")` is called again while the first call's run is still going
- **THEN** both responses carry the same `run_id` and the script runs once

#### Scenario: Finished run is retrievable within the hour

- **WHEN** a run finished 10 minutes ago
- **THEN** `iris_run_result` returns its final response
