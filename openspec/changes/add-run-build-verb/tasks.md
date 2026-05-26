## 1. Verb implementation

- [x] 1.1 `internal/verbs/run_build.go` — `RunBuild(ctx, client, taskID, opts) (*RunBuildResult, error)` with `RunBuildOptions{Target string}` and `RunBuildResult{Command, ExitCode, Output}`
- [x] 1.2 `internal/verbs/build_locks.go` — separate `buildLocks sync.Map` + `lockWorktree(path) *sync.Mutex` (mirrors `lockSourceRepo`)
- [x] 1.3 Build-command resolution: `script/iris-build` (executable) → `Makefile` → actionable error naming both
- [x] 1.4 Run via `exec.CommandContext` with `CombinedOutput()` in `resolved.WorktreePath`
- [x] 1.5 `BuildExitError` type wrapping `*RunBuildResult` so callers can `errors.As` and still see output/exit code

## 2. Tests

- [x] 2.1 Happy build via `script/iris-build` (executable shim)
- [x] 2.2 Happy build via Makefile fallback (no script present)
- [x] 2.3 Neither script nor Makefile present — error names both paths
- [x] 2.4 Non-zero exit returns `*BuildExitError` + populated result with output captured
- [x] 2.5 `target` argument passed through to the script
- [x] 2.6 Concurrent builds on DIFFERENT worktrees of the same source repo run in parallel (not serialized)

## 3. CLI + MCP wiring

- [x] 3.1 `internal/mcp/handler_run_build.go` — `NewRunBuildHandler(client)`; on `*BuildExitError` returns ErrorResponse that includes ExitCode + Output
- [x] 3.2 `cmd/iris/run_build.go` — `newRunBuildCmd()` with optional `target` positional
- [x] 3.3 Register `iris_run_build` in `internal/daemon/run.go` (handler + toolDefinitions)

## 4. Validation

- [x] 4.1 `go vet ./...` clean
- [x] 4.2 `make test` passes under -race
- [x] 4.3 `openspec validate --all --strict` passes
