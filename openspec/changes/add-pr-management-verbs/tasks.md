# Implementation tasks: add-pr-management-verbs

**Design doc:** `openspec/changes/add-pr-management-verbs/design.md`

## 1. Failing tests

- [x] 1.1 Write failing tests for `internal/verbs/gh_pr_view_test.go` covering every scenario in `specs/iris-gh-pr-view/spec.md`. Use the existing fake-gh PATH override pattern from v1.0
- [x] 1.2 Write failing tests for `internal/verbs/gh_pr_ready_test.go` covering `specs/iris-gh-pr-ready/spec.md`
- [x] 1.3 Write failing tests for `internal/verbs/gh_pr_comment_test.go` covering `specs/iris-gh-pr-comment/spec.md`
- [x] 1.4 Write failing tests for `internal/verbs/gh_pr_close_test.go` covering `specs/iris-gh-pr-close/spec.md`
- [x] 1.5 Confirm every `it should X` acceptance criterion in `design.md` has a corresponding failing test (Prove-It Pattern)

## 2. `iris:gh_pr_view`

**Depends on:** Stage 1

- [x] 2.1 Implement `internal/verbs/gh_pr_view.go`: `func GhPrView(ctx context.Context, in GhPrViewInput) (*GhPrViewResult, error)`
- [x] 2.2 Resolve source repo via `verbs.Resolve(taskID)`; enforce argus project allowlist; acquire per-source-repo lock
- [x] 2.3 Shell out: `gh pr view <pr_number> --json state,checks,reviews,mergeable,headRefName,baseRefName,isDraft,statusCheckRollup` in the resolved source repo
- [x] 2.4 Parse stdout as JSON; return the parsed object
- [x] 2.5 Return structured error on non-zero gh exit, carrying gh's stdout and stderr
- [x] 2.6 Verify Stage 1.1 tests pass

## 3. `iris:gh_pr_ready`

**Depends on:** Stage 1

- [x] 3.1 Implement `internal/verbs/gh_pr_ready.go`: `func GhPrReady(ctx context.Context, in GhPrReadyInput) (*GhPrReadyResult, error)`
- [x] 3.2 Source-repo resolution + allowlist + lock (same as 2.2)
- [x] 3.3 Pre-fetch draft state via `gh pr view <pr_number> --json isDraft` so the result can include `was_draft`
- [x] 3.4 Shell out: `gh pr ready <pr_number>`
- [x] 3.5 Return `{ ready: true, was_draft }` on success; structured error on non-zero
- [x] 3.6 Verify Stage 1.2 tests pass

## 4. `iris:gh_pr_comment`

**Depends on:** Stage 1

- [x] 4.1 Implement `internal/verbs/gh_pr_comment.go`: `func GhPrComment(ctx context.Context, in GhPrCommentInput) (*GhPrCommentResult, error)`
- [x] 4.2 Input validation: refuse empty `body` before shelling out
- [x] 4.3 Source-repo resolution + allowlist + lock
- [x] 4.4 Shell out: `gh pr comment <pr_number> --body <body>`
- [x] 4.5 Parse stdout to extract the comment URL; fall back to `parse_warning` on failure
- [x] 4.6 Verify Stage 1.3 tests pass

## 5. `iris:gh_pr_close`

**Depends on:** Stage 1

- [x] 5.1 Implement `internal/verbs/gh_pr_close.go`: `func GhPrClose(ctx context.Context, in GhPrCloseInput) (*GhPrCloseResult, error)`
- [x] 5.2 Source-repo resolution + allowlist + lock
- [x] 5.3 Shell out: `gh pr close <pr_number>` (append `--delete-branch` when `delete_branch = true`)
- [x] 5.4 Return `{ closed: true, branch_deleted }`
- [x] 5.5 Verify Stage 1.4 tests pass

## 6. MCP handlers, Cobra subcommands, daemon registration

**Depends on:** Stages 2, 3, 4, 5

- [x] 6.1 Implement `internal/mcp/handler_gh_pr_view.go`, `handler_gh_pr_ready.go`, `handler_gh_pr_comment.go`, `handler_gh_pr_close.go` following the existing handler pattern
- [x] 6.2 Implement `cmd/iris/gh_pr_view.go`, `gh_pr_ready.go`, `gh_pr_comment.go`, `gh_pr_close.go` following the existing direct-CLI pattern
- [x] 6.3 Register the 4 new tools in `internal/daemon/run.go`'s `toolDefinitions()`
- [x] 6.4 Update `README.md` CLI section
- [x] 6.5 Run `make test` under `-race`; verify all stages pass
- [x] 6.6 Run `openspec validate add-pr-management-verbs --strict`
