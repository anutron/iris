## 1. Verb implementation

- [x] 1.1 `internal/verbs/gh_pr_merge.go` — `GHPRMerge(ctx, client, taskID, opts) (*GHPRMergeResult, error)` with `GHPRMergeOptions{PRNumber, Strategy}` and `GHPRMergeResult{Merged, Strategy}`
- [x] 1.2 Validate `pr_number > 0` at the verb boundary
- [x] 1.3 Validate `strategy` against the squash|merge|rebase enum via exported `IsValidGHPRMergeStrategy`
- [x] 1.4 Shell out to `gh pr merge <N> --<strategy>` in the source repo via `exec.CommandContext`

## 2. Tests

- [x] 2.1 Happy squash merge — argv contains `--squash`
- [x] 2.2 Rebase strategy — argv contains `--rebase`
- [x] 2.3 Merge strategy — argv contains `--merge`
- [x] 2.4 gh non-zero exit surfaces stderr in the verb error
- [x] 2.5 Invalid strategy rejected BEFORE gh invocation (argv file absent)
- [x] 2.6 Refuses unknown task ID
- [x] 2.7 Rejects `pr_number <= 0` at the verb boundary

## 3. CLI + MCP wiring

- [x] 3.1 `internal/mcp/handler_gh_pr_merge.go` — `NewGHPRMergeHandler(client)` with `iris:gh_pr_merge:` error prefix; defaults `strategy` to `"squash"` when empty
- [x] 3.2 `cmd/iris/gh_pr_merge.go` — `newGHPRMergeCmd()` with `--pr` (required int) and `--strategy` (default "squash") flags
- [x] 3.3 Register `iris_gh_pr_merge` in `internal/daemon/run.go` (handler + toolDefinitions with `enum` constraint)
- [x] 3.4 Add subcommand to `cmd/iris/main.go` (orchestrator owns this file)
- [x] 3.5 Add CLI command to `README.md` (orchestrator owns this file)

## 4. Validation

- [x] 4.1 `go vet ./...` clean
- [x] 4.2 `make test` passes under -race
- [x] 4.3 `openspec validate --all --strict` passes
