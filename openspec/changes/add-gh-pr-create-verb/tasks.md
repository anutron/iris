## 1. Verb implementation

- [x] 1.1 `internal/verbs/gh_pr_create.go` — `GHPRCreate(ctx, client, taskID, opts) (*GHPRCreateResult, error)` with `GHPRCreateOptions{Title, Body, Draft}` and `GHPRCreateResult{Number, URL}`
- [x] 1.2 Refuse default branch (use `verbs.DefaultBranch`)
- [x] 1.3 Shell out to `gh pr create` in the source repo via `exec.CommandContext`
- [x] 1.4 Parse PR number from `/pull/(\d+)` regex on the last non-empty stdout line

## 2. Tests

- [x] 2.1 Happy create — fake gh returns a PR URL, verb parses number=42
- [x] 2.2 Refuses default branch (fake gh NOT invoked; argv file absent)
- [x] 2.3 Draft flag respected — `--draft` present iff `Draft: true`
- [x] 2.4 Empty body OK — `--body` flag omitted when body is empty
- [x] 2.5 `gh auth login` surfaced in error when gh exits non-zero with that stderr
- [x] 2.6 Refuses unknown task ID
- [x] 2.7 Title required at the verb boundary

## 3. CLI + MCP wiring

- [x] 3.1 `internal/mcp/handler_gh_pr_create.go` — `NewGHPRCreateHandler(client)` with `iris:gh_pr_create:` error prefix
- [x] 3.2 `cmd/iris/gh_pr_create.go` — `newGHPRCreateCmd()` with `--title` (required), `--body`, `--draft` flags
- [x] 3.3 Register `iris_gh_pr_create` in `internal/daemon/run.go` (handler + toolDefinitions)
- [x] 3.4 Add subcommand to `cmd/iris/main.go` (orchestrator owns this file)
- [x] 3.5 Add CLI command to `README.md` (orchestrator owns this file)

## 4. Test helpers

- [x] 4.1 `internal/verbs/fake_gh_test.go` — `writeFakeGH(t, scriptBody) string` and `readFakeGHArgv(t, dir) string`

## 5. Validation

- [x] 5.1 `go vet ./...` clean
- [x] 5.2 `make test` passes under -race
- [x] 5.3 `openspec validate --all --strict` passes
