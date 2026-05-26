## 1. Verb implementation

- [x] 1.1 `internal/verbs/push.go` — `Push(ctx, client, taskID, opts) (*PushResult, error)` with `PushOptions{ForceWithLease bool}` and `PushResult{Pushed, Branch, RemoteSHA}`
- [x] 1.2 Refuse default branch (use `verbs.DefaultBranch`)
- [x] 1.3 Hold `lockSourceRepo` for the duration of the push
- [x] 1.4 Read `git rev-parse origin/<branch>` post-push for the remote SHA

## 2. Tests

- [x] 2.1 Happy push (worktree commits land on bare origin)
- [x] 2.2 Refuses default branch (no push attempted, origin unchanged)
- [x] 2.3 Refuses unknown task ID
- [x] 2.4 `force_with_lease=true` succeeds when remote tracks correctly
- [x] 2.5 Non-fast-forward without `force_with_lease` errors clearly
- [x] 2.6 Allowlist rejection

## 3. CLI + MCP wiring

- [x] 3.1 `internal/mcp/handler_push.go` — `NewPushHandler(client)`
- [x] 3.2 `cmd/iris/push.go` — `newPushCmd()` with `--force-with-lease` flag
- [x] 3.3 Register `iris_push` in `internal/daemon/run.go` (handler + toolDefinitions)
- [x] 3.4 Add subcommand to `cmd/iris/main.go`
- [x] 3.5 Add CLI command to `README.md`

## 4. Validation

- [x] 4.1 `go vet ./...` clean
- [x] 4.2 `make test` passes under -race
- [x] 4.3 `openspec validate --all --strict` passes
