## Context

`iris:run_build` is the fourth iris verb. Like `merge_to_master` and `push`, it exists because argus's sandbox can't safely run host-level operations on behalf of an agent — in this case, executing the project's build toolchain. Builds frequently shell out to host-installed compilers, package managers, and native tools; the agent's sandbox doesn't expose those, and even when it could, agents shouldn't be the ones holding the keys to "compile and link arbitrary native code."

The verb is narrow: one build per call, capture combined output, return exit code. Streaming, parallelism, and incremental build awareness are all out of scope for v1.

## Goals / Non-Goals

**Goals:**

- Run a project's canonical build command in the WORKTREE (not the source repo) so the agent's in-progress changes are what gets built.
- Discover the build command by convention: `script/iris-build` first (executable script), then `Makefile` (the `build` target). Refuse with an actionable error if neither exists.
- Serialize concurrent builds against the same WORKTREE (a target dir can only have one writer); allow concurrent builds across DIFFERENT worktrees of the same source repo.
- Return enough structure for the caller to distinguish "build ran and failed" from "iris refused to run the build" — both with the command, the exit code, and the captured output.

**Non-Goals:**

- Streaming output. Argus's SSE plumbing for tool callbacks doesn't exist yet on the iris side; adding it is a separate change. Captured output on completion is the v1 contract.
- Multiple build targets in a single call. The `target` arg is a single string passed verbatim to the script/Makefile.
- Build-system autodetection beyond the two conventions (script/iris-build, Makefile). If a project uses `npm run build` or `cargo build` natively, it adds `script/iris-build` as a one-line shim.
- Caching, parallelism, incremental hints. Iris does not interpret build output.

## Decisions

### Build runs in the WORKTREE, not the source repo

The worktree is where the agent's commits live. Running the build in the source repo would build the previous state of the project, defeating the point. The argus worktree pattern guarantees the working tree at `task.WorktreePath` is the agent's in-progress branch.

### Per-WORKTREE mutex, separate from the per-source-repo mutex

`merge_to_master` and `push` mutate the source repo's git state, so they share `repoLocks` keyed on `SourceRepo`. `run_build` writes to the worktree's `target/`, `node_modules/`, `dist/`, etc. — those are per-worktree. Two builds in DIFFERENT worktrees of the same source repo are independent and should run concurrently. Two builds in the SAME worktree (e.g., a caller-induced double-call) MUST serialize so they don't fight over the build output dir.

A new `var buildLocks sync.Map` lives in `internal/verbs/build_locks.go` (separate file so the lock-table file stays single-purpose). `lockWorktree(path)` mirrors `lockSourceRepo(path)`.

### Build-command resolution: `script/iris-build` then `Makefile`

`script/iris-build` is the project-owned convention — a project that wants iris to build it drops one executable file in `script/`. This matches the `scripts-to-rule-them-all` pattern and gives projects total control over the actual build commands.

`Makefile` is the universal fallback: if a project already has a `make build` target, iris uses it without requiring a new file. Most projects in Aaron's spectrum either have a Makefile (Go services, MCP servers) or can trivially add `script/iris-build` (Next.js, Rails).

If neither exists, iris errors with an actionable message naming BOTH paths so the operator knows the two ways to opt in.

The executable check on `script/iris-build` uses `os.Stat` + `mode&0o111 != 0`. A non-executable file is treated as "not present" and falls through to the Makefile check.

### Streaming deferred; v1 uses `CombinedOutput`

Argus already streams agent stdout/stderr to the user during agent runs, but iris's MCP callback channel returns a single response per call. Adding SSE on iris's side is doable but cross-cuts the registrar and the MCP server. The v1 contract is "build runs to completion, you get the combined output." If the build is long, the caller's context timeout governs.

`cmd.CombinedOutput()` interleaves stdout and stderr in the order the process emitted them, which is the most useful format for a human reading the result.

### Non-zero exit returns BOTH the result and a typed error

If the build runs and exits non-zero (the common case: compile error, test failure, lint break), the verb returns a populated `*RunBuildResult{Command, ExitCode, Output}` AND a typed `*BuildExitError` that wraps the result. Callers can:

- `errors.As(err, &buildErr)` to detect the "build ran but failed" case and access the result.
- Read `result.Output` and `result.ExitCode` to surface compile errors back to the agent.

The MCP handler treats this as an error response (so argus surfaces "tool failed" to the caller) but includes Output and ExitCode in the error message so the agent sees the compile errors without a second round-trip.

If the build can't even start (no build mechanism, exec error, context cancellation before the process spawned), the verb returns `(nil, error)` with no result. The handler surfaces this as an error response with just the message.

### Why a custom error type over a sentinel

A sentinel (`var ErrBuildFailed = errors.New(...)`) would force callers to a `errors.Is` check and then a second lookup for the result. A custom `*BuildExitError` lets callers do one `errors.As` and have the structured result in hand. This is the cleaner pattern when the error carries data, per [Go error handling proverbs](https://go.dev/blog/error-handling-and-go).

## Risks / Trade-offs

- **[Risk] Build hangs forever.** Mitigation: the verb uses `exec.CommandContext`; the caller's context timeout kills the process. The MCP handler inherits the argus tool-call timeout (configured argus-side).
- **[Risk] Output exceeds buffer.** Go's `CombinedOutput()` buffers the full output in memory. For pathological builds (gigabytes of warnings), this could OOM iris. Mitigation: deferred — streaming is the long-term fix; v1 documents the trade.
- **[Trade-off] No build-system autodetection.** A project must add `script/iris-build` or have a `Makefile` with a `build` target. The error message names both options; the bar is low. Adding more conventions later is non-breaking.
- **[Trade-off] No streaming in v1.** Caller waits until the build finishes. Long builds are exposed to caller timeouts. The contract documents this so callers can budget appropriately.
- **[Trade-off] Per-worktree mutex doesn't prevent the agent from invoking a build inside the worktree concurrently with iris's build.** Iris doesn't have a global "this worktree is building" sentinel visible from inside the sandbox. The mutex protects iris-against-iris concurrent calls only.

## Migration Plan

- Verb-only addition. No installer changes, no host state, no schema migration.
- Rollback: revert the commit; the daemon stops registering `iris_run_build` and the tool disappears from argus's allowlist on next heartbeat.

## Open Questions

None — v1 contract is settled. Streaming is the obvious follow-up but cleanly additive (new field, no break).
