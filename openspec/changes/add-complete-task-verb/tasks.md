## 1. Argus client extensions

- [x] 1.1 `SetTaskStatus(ctx, taskID, status)` posts to `/api/tasks/<id>/status`
- [x] 1.2 `ArchiveTask(ctx, taskID)` posts to `/api/tasks/<id>/archive`

## 2. Verb implementation

- [x] 2.1 `internal/verbs/complete_task.go` with `CompleteTask`, `CompleteTaskOptions`, `CompleteTaskResult`, checkpoint constants
- [x] 2.2 Strategy validation (`no_ff` | `ff_only`)
- [x] 2.3 Already-complete shortcut returning all checkpoints
- [x] 2.4 Default-branch push under the per-source-repo mutex
- [x] 2.5 Remote-task-branch delete tolerates "already gone"
- [x] 2.6 Archive failure is non-fatal (surfaces in result.Error, not error)

## 3. Tests

- [x] 3.1 Happy full path (5 checkpoints, argus task marked complete + archived, origin's default branch advanced, remote task branch deleted)
- [x] 3.2 Already-complete task returns success with all checkpoints
- [x] 3.3 Partial failure returns checkpoints reached (status-update fails)
- [x] 3.4 Resume after partial succeeds OR documents current limitation
- [x] 3.5 Archive failure returns success with result.Error populated and 4 checkpoints
- [x] 3.6 Invalid merge strategy rejected upfront

## 4. CLI + MCP wiring

- [x] 4.1 `internal/mcp/handler_complete_task.go`
- [x] 4.2 `cmd/iris/complete_task.go` with `--merge-strategy` flag
- [x] 4.3 Register `iris_complete_task` in `internal/daemon/run.go`
- [x] 4.4 Add subcommand to `cmd/iris/main.go`

## 5. Validation

- [x] 5.1 `go vet ./...` clean
- [x] 5.2 `make test` passes under -race
- [x] 5.3 `openspec validate --all --strict` passes
