## Why

Agents inside argus worktrees cannot reliably run a project's build because the sandbox cannot allow arbitrary subprocess execution against host toolchains (xcrun, node-gyp, system linkers, brew-installed deps, etc.). Today agents either skip the build step or hand-roll partial builds that miss native steps. `iris:run_build` is the host-side verb that closes the loop: it runs the canonical build command for the task's worktree on the host, captures combined output, and returns the exit code.

## What Changes

- Add `iris:run_build` verb. Resolves source repo from `task_id`, looks for `script/iris-build` (preferred) or `Makefile` (fallback) in the worktree, runs the build in the worktree under a per-worktree mutex, returns command + exit code + combined output.
- Wire MCP handler `iris_run_build` and CLI subcommand `iris run-build <task-id> [target]`.

## Capabilities

### New Capabilities

- `iris-run-build`: The `iris:run_build` verb specifically — input contract, build-command resolution order, per-worktree mutex, structured response with command, exit code, and combined output.

### Modified Capabilities

None.

## Impact

- No new host state, no installer change.
- Build runs in the WORKTREE, not the source repo: per-worktree lock granularity (distinct from the per-source-repo git mutex).
- Streaming is deferred to a follow-up; v1 returns combined output on completion via `cmd.CombinedOutput()`.
- Non-zero exit returns BOTH a populated result (with output) AND a typed error (`*BuildExitError`) so callers can `errors.As` and still see the build output.
