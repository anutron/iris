## Why

Argus caps every plugin tool call at 30 seconds (`DefaultInvokeTimeout`, hardcoded), and iris's own callback listener has a 30-second `WriteTimeout`. `iris_run_checks` and `iris_run_build` block until the script exits and run it on the request context. As a result, any check or build that takes longer than 30 seconds fails with `context deadline exceeded`, and iris kills the script when argus gives up. Grandchildren such as jest workers can outlive it while iris holds the worktree lock. This was reproduced on 2026-10-19: a 45-second check failed at exactly 30 seconds and never finished. Sketch's `yarn-test` hit the same failure in the October 18–19 session.

## What Changes

- `iris_run_checks` and `iris_run_build` run their script detached from the caller, under an iris-owned 30-minute ceiling, with bounded pipe draining so orphaned grandchildren can't hold the call open.
- Both tools accept `wait_seconds` (default 20, capped at 25). A run that finishes in that window returns exactly today's response. A run still going returns `{run_id, status: "running", ...}` instead of an error.
- A new `iris_run_result(run_id, wait_seconds)` tool long-polls a run and returns its final response once it finishes.
- A repeat call for the same task, tool and argument while a run is in progress returns the same `run_id` instead of queueing a second run behind the worktree lock.
- Runs live in memory and finished runs are kept for an hour. After a daemon restart, `iris_run_result` reports the run as unknown.
- `iris_run_checks` gets a base spec. It shipped without one.

## Capabilities

### New Capabilities

- `iris-worktree-runs`: detached execution, the bounded wait and `run_id` handle, `iris_run_result`, deduplication and retention.
- `iris-run-checks`: the `iris:run_checks` verb contract.

### Modified Capabilities

- `iris-run-build`: `iris:run_build` runs as a worktree run.

## Impact

- `internal/verbs/run_checks.go`, `run_build.go`: detached context, ceiling, `WaitDelay`.
- New `internal/mcp/runs.go` (run registry) and `handler_run_result.go`. The run_checks and run_build handlers change, as do the `internal/daemon/run.go` registration and tool schemas.
- `claude/skills/iris/SKILL.md`.
- The CLI keeps its synchronous behaviour, and it gains the ceiling and pipe bound.
- The change is additive for callers: fast runs answer exactly as before.
