## Context

Two 30-second limits sit on every iris MCP call: argus's plugin `DefaultInvokeTimeout` (an `http.Client` timeout, not configurable per tool) and iris's own `http.Server` `WriteTimeout`. `RunChecks` and `RunBuild` pass the request context straight to `exec.CommandContext`, so when argus gives up, the script is SIGKILLed. With no `WaitDelay`, `CombinedOutput` then waits for any grandchild still holding the pipe, while holding the per-worktree lock. `push`, `fetch`, `publish` and `reload` already detach from caller cancellation (`resilient-push-fetch`, `resilient-publish-reload`). Those changes named an async model for long verbs as future work, and this change builds it for the two worktree-run verbs.

## Goals / Non-Goals

**Goals:**

- A check or build of any reasonable length completes and its result reaches the caller.
- Fast runs (tsc, lint) stay a single call with an unchanged response.
- No unbounded growth, and no pile-up of duplicate runs behind the worktree lock.

**Non-Goals:**

- Async handling for publish, reload, `ship_feature` or `merge_to_master`'s post-merge hook. Those already detach their work, and their recovery path is `iris_status`. The registry is reusable if one needs it later.
- Persisting runs across daemon restarts.
- Changing argus's 30-second cap. That is argus's contract, and a longer blocking call would still tie up the agent.
- Cancelling a run.

## Decisions

### Hybrid: wait briefly, then hand back a handle

The handler starts the run in a goroutine and waits up to `wait_seconds`. The default is 20 and the cap is 25, which leaves margin under both 30-second limits for resolution and transport. Finished runs return the verb's existing response. Unfinished runs return a `running` handle. Pure start-then-poll (the `plannotator_*` shape) would cost every fast check an extra round trip for no benefit. Plannotator waits on humans, but most checks finish in seconds.

### The registry lives in the MCP layer and stores final responses

`mcp.RunRegistry` stores each run's finished `mcp.Response`, produced by the same formatting code the handlers use today. So `iris_run_result` returns byte-for-byte what a synchronous call would have returned. The verbs stay synchronous Go functions, and the CLI is unchanged.

### Detach and bound inside the verbs

`RunChecks` and `RunBuild` exec under `context.WithTimeout(context.WithoutCancel(ctx), 30m)` with `cmd.WaitDelay = 1s`, following the `runGitTransfer` precedent. The 30-minute ceiling is a hang guard, not a budget. It's a constant rather than `.iris.toml` config because no project has needed a different value yet, and adding a field later is non-breaking. `[build] timeout_seconds` was rejected because it budgets reload's build step, and coupling a test suite's ceiling to it would surprise people. A ceiling hit returns an error that says `[timeout]`.

### Deduplicate in-progress runs by (tool, task_id, argument)

While a run with the same key is in progress, a new call joins it and returns its `run_id`. Without this, an agent that retries after a `running` handle would queue a second full suite behind the worktree lock. Once the run finishes, the key is free.

### Retention and identity

`run_id` is 16 random bytes in hex. Finished runs are pruned an hour after they finish, on each registry access. Unknown or expired IDs get an error that says iris may have restarted or the run expired, and suggests re-running.

### The repo is reported on every response for the run

Each run records its resolution into its own `RepoRecorder`. The run_checks, run_build and run_result handlers copy it into the request's recorder, so the `name-resolved-repo` annotation names the repo on running, finished and polled responses alike.

## Risks / Trade-offs

- **A daemon restart loses in-flight results.** The caller gets an explicit "unknown run_id" and re-runs. This is acceptable because runs are read-only checks.
- **An agent that never polls leaves the run going** until it finishes or hits the ceiling. Dedup stops repeats from piling up.
- **`WaitDelay` closes pipes on a hung grandchild without killing it.** The same trade-off was accepted for git transfers.
