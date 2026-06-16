**Design doc:** `openspec/changes/add-push-remote-and-base-repo/design.md`

## 1. Tests

- [x] 1.1 Add failing test in `internal/verbs/push_test.go`: `remote` override pushes the effective branch to the named remote (a second bare remote), not origin, and the result reports `remote`
- [x] 1.2 Add failing test in `internal/verbs/push_test.go`: a `remote` that is not a configured remote is refused (no push, origin and the named ref unchanged)
- [x] 1.3 Add failing test in `internal/verbs/push_test.go`: a `remote` beginning with `-` is rejected before git runs
- [x] 1.4 Add failing test in `internal/verbs/push_test.go`: omitted `remote` preserves today's push-to-origin behavior
- [x] 1.5 Add failing test in `internal/verbs/gh_pr_create_crossfork_test.go`: `base_repo` opens a same-repo PR on that repo (`--repo <base_repo> --head <effective>`, no `--base`, no fork-qualified head) and does NOT consult fork detection
- [x] 1.6 Add failing test: `base_repo` on a fork origin still yields the same-repo-on-base_repo invocation (precedence over auto-detection)
- [x] 1.7 Add failing test: a `base_repo` beginning with `-` or not `owner/repo` shaped is rejected before gh runs
- [x] 1.8 Add failing test: omitted `base_repo` preserves today's same-repo / cross-fork behavior
- [x] 1.9 Confirm every acceptance criterion in `design.md` maps to a test (Prove-It Pattern)

## 2. Push remote override

**Depends on:** Stage 1

- [x] 2.1 Add `Remote string` to `PushOptions` and `Remote string` to `PushResult` in `internal/verbs/push.go`
- [x] 2.2 Compute `remote := "origin"; if opts.Remote != "" { remote = opts.Remote }`; reject leading-dash; validate via `git remote get-url <remote>`; use `remote` for the push args, the `git rev-parse <remote>/<branch>`, and the result `Remote`
- [x] 2.3 Add `remote` (string, optional) to `pushInput` in `internal/mcp/handler_push.go` and pass it into `PushOptions`
- [x] 2.4 Add `--remote` flag to `newPushCmd` in `cmd/iris/push.go` and pass it into `PushOptions`
- [x] 2.5 Update the `iris:push` MCP tool schema + description to advertise the optional `remote` parameter and the origin-default model
- [x] 2.6 Run the stage-1 push tests; confirm green

## 3. PR-create base_repo override

**Depends on:** Stage 1

- [x] 3.1 Add `BaseRepo string` to `GHPRCreateOptions` in `internal/verbs/gh_pr_create.go`
- [x] 3.2 Validate `base_repo` (reject leading-dash; require `owner/repo` shape); when non-empty, build `gh pr create --repo <base_repo> --head <effective> --title ...` (omit `--base`, no fork qualification) and skip `detectForkUpstream`
- [x] 3.3 Add `base_repo` (string, optional) to `ghPRCreateInput` in `internal/mcp/handler_gh_pr_create.go` and pass it into `GHPRCreateOptions`
- [x] 3.4 Add `--base-repo` flag to `newGHPRCreateCmd` in `cmd/iris/gh_pr_create.go` and pass it into `GHPRCreateOptions`
- [x] 3.5 Update the `iris:gh_pr_create` MCP tool schema + description to advertise `base_repo` and the cross-fork-vs-same-repo model
- [x] 3.6 Run the stage-1 PR-create tests; confirm green

## 4. Docs

**Depends on:** Stage 2, Stage 3

- [x] 4.1 Update `README.md` CLI table (`--remote`, `--base-repo`) and add a short "push to upstream + same-repo PR" workflow note
- [x] 4.2 Update `claude/skills/iris/SKILL.md` with the push-to-upstream workflow

## 5. Verify

**Depends on:** Stage 2, Stage 3, Stage 4

- [x] 5.1 Run the full `go test ./...` suite; confirm green
- [x] 5.2 Run `go vet ./...` and `gofmt -l` on changed files; confirm clean
- [x] 5.3 Run `openspec validate add-push-remote-and-base-repo --strict`; confirm clean
