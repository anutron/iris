**Design doc:** `openspec/changes/add-push-pr-branch-overrides/design.md`

## 1. Tests

- [ ] 1.1 Add failing test in `internal/verbs/push_test.go`: `branch` override pushes the named branch instead of the resolved task branch, and the result reports the override branch
- [ ] 1.2 Add failing test in `internal/verbs/push_test.go`: `branch` override equal to the default branch is refused (no push)
- [ ] 1.3 Add failing test in `internal/verbs/push_test.go`: omitted `branch` preserves today's resolve-from-task behavior
- [ ] 1.4 Add failing test in `internal/verbs/gh_pr_create_test.go`: `head` override opens the PR for the named branch (same-repo `--head <override>`)
- [ ] 1.5 Add failing test in `internal/verbs/gh_pr_create_test.go`: `head` override equal to the default branch is refused (no gh invocation)
- [ ] 1.6 Add failing test in `internal/verbs/gh_pr_create_test.go`: fork origin fork-qualifies the `head` override (`<fork-owner>:<override>`)
- [ ] 1.7 Add failing test in `internal/verbs/gh_pr_create_test.go`: omitted `head` preserves today's resolve-from-task behavior
- [ ] 1.8 Confirm every acceptance criterion in `design.md` maps to a failing test (Prove-It Pattern)

## 2. Push branch override

**Depends on:** Stage 1

- [ ] 2.1 Add `Branch string` to `PushOptions` in `internal/verbs/push.go`
- [ ] 2.2 Compute `effective := resolved.Branch; if opts.Branch != "" { effective = opts.Branch }`; use `effective` for the empty-branch guard, default-branch refusal, push args, rev-parse, and result `Branch`
- [ ] 2.3 Add `branch` (string, optional) to `pushInput` in `internal/mcp/handler_push.go` and pass it into `PushOptions`
- [ ] 2.4 Add `--branch` flag to `newPushCmd` in `cmd/iris/push.go` and pass it into `PushOptions`
- [ ] 2.5 Update the `iris:push` MCP tool schema to advertise the optional `branch` parameter
- [ ] 2.6 Run the stage-1 push tests; confirm green

## 3. PR-create head override

**Depends on:** Stage 1

- [ ] 3.1 Add `Head string` to `GHPRCreateOptions` in `internal/verbs/gh_pr_create.go`
- [ ] 3.2 Compute `effective := resolved.Branch; if opts.Head != "" { effective = opts.Head }`; use `effective` for the empty-branch guard, default-branch refusal, the same-repo `--head`, and the fork-qualified `fu.ForkOwner+":"+effective`
- [ ] 3.3 Add `head` (string, optional) to `ghPRCreateInput` in `internal/mcp/handler_gh_pr_create.go` and pass it into `GHPRCreateOptions`
- [ ] 3.4 Add `--head` flag to `newGHPRCreateCmd` in `cmd/iris/gh_pr_create.go` and pass it into `GHPRCreateOptions`
- [ ] 3.5 Update the `iris:gh_pr_create` MCP tool schema to advertise the optional `head` parameter
- [ ] 3.6 Run the stage-1 PR-create tests; confirm green

## 4. Verify

**Depends on:** Stage 2, Stage 3

- [ ] 4.1 Run the full `go test ./...` suite; confirm green
- [ ] 4.2 Run `go vet ./...` and `gofmt -l` on changed files; confirm clean
- [ ] 4.3 Run `openspec validate add-push-pr-branch-overrides --strict`; confirm clean
