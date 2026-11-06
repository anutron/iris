## MODIFIED Requirements

### Requirement: Worktree runs outlive the caller

`iris:run_checks` and `iris:run_build` SHALL run their script detached from the caller's cancellation, under an iris-owned ceiling of 30 minutes that covers both waiting for the per-worktree lock and running the script. A run that exceeds the ceiling while still waiting for the lock SHALL fail with a `[timeout]` error naming the worktree lock, without running the script. They SHALL bound waiting on output pipes after the script exits or is killed, so an orphaned grandchild process can't hold the call or the per-worktree lock open. They SHALL start the script as the leader of its own process group. A run that exceeds the ceiling SHALL kill that entire process group with SIGKILL, so no descendant that stayed in the group survives the run, and SHALL fail with an error containing `[timeout]` and the captured output.

#### Scenario: Caller cancellation does not kill the run

- **WHEN** the caller's context is cancelled while `script/iris-check` is running
- **THEN** the script continues to completion and its result is recorded

#### Scenario: Lock wait counts against the ceiling

- **WHEN** another run holds the worktree lock for longer than the ceiling
- **THEN** the waiting run fails with a `[timeout]` error naming the worktree lock, and its script never runs

#### Scenario: Orphaned grandchild does not hold the run open

- **WHEN** the script exits while a background child it started keeps the output pipe open
- **THEN** the run finishes within a few seconds of the script's exit instead of waiting for the grandchild

#### Scenario: Ceiling kills grandchildren

- **WHEN** a run's script starts a child that starts its own long-running child, and the run hits the ceiling
- **THEN** the run fails with `[timeout]`, and neither the script, the child nor the grandchild is still running afterwards

#### Scenario: Ceiling kill applies to builds too

- **WHEN** an `iris_run_build` script leaves a long-running descendant and hits the ceiling
- **THEN** the descendant is killed along with the script

### Requirement: Duplicate runs share a handle and finished runs expire

While a run is in progress, a call to the same tool with the same `task_id` and the same `check` and `args` (for `iris_run_checks`) or the same `target` (for `iris_run_build`) SHALL join that run and return its `run_id` instead of starting another. Two `iris_run_checks` calls with the same `check` but different `args`, including the same args in a different order, SHALL be different runs. Finished runs SHALL remain retrievable for one hour after they finish and SHALL then be discarded.

#### Scenario: Retry joins the in-progress run

- **WHEN** `iris_run_checks(task_id=T, check="test")` is called again while the first call's run is still going
- **THEN** both responses carry the same `run_id` and the script runs once

#### Scenario: Retry with the same args joins the run

- **WHEN** `iris_run_checks(task_id=T, check="test", args=["spec/a_spec.rb"])` is called again with the same args while the first run is going
- **THEN** both responses carry the same `run_id` and the script runs once

#### Scenario: Different args are a different run

- **WHEN** `iris_run_checks(task_id=T, check="test", args=["spec/a_spec.rb"])` is called while a run with `args=["spec/b_spec.rb"]` is in progress
- **THEN** the call starts its own run with a different `run_id`, queued behind the first under the worktree lock

#### Scenario: Args cannot collide across the boundary

- **WHEN** one call passes `check="a"` with `args=["b"]` and another passes `check="a b"` with no args
- **THEN** they are different runs

#### Scenario: Finished run is retrievable within the hour

- **WHEN** a run finished 10 minutes ago
- **THEN** `iris_run_result` returns its final response
