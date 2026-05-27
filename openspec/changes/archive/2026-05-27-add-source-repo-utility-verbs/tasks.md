# Implementation tasks: add-source-repo-utility-verbs

**Design doc:** `openspec/changes/add-source-repo-utility-verbs/design.md`

## 1. Failing tests

- [ ] 1.1 Write failing tests for `internal/verbs/fetch_test.go` covering every scenario in `specs/iris-fetch/spec.md`. Use real git against tempdirs with a bare origin
- [ ] 1.2 Write failing tests for `internal/verbs/branch_delete_remote_test.go` covering `specs/iris-branch-delete-remote/spec.md`
- [ ] 1.3 Write failing tests for `internal/verbs/tag_test.go` covering `specs/iris-tag/spec.md`
- [ ] 1.4 Confirm every `it should X` acceptance criterion in `design.md` has a corresponding failing test (Prove-It Pattern)

## 2. `iris:fetch`

**Depends on:** Stage 1

- [ ] 2.1 Implement `internal/verbs/fetch.go`: `func Fetch(ctx context.Context, in FetchInput) (*FetchResult, error)`
- [ ] 2.2 Source-repo resolution + allowlist + lock
- [ ] 2.3 Capture pre-fetch refs via `git ls-remote origin` (so the post-fetch diff produces `refs_updated`)
- [ ] 2.4 Shell out: `git fetch origin`
- [ ] 2.5 Compute `refs_updated` by comparing pre/post `git for-each-ref refs/remotes/origin`
- [ ] 2.6 Verify Stage 1.1 tests pass

## 3. `iris:branch_delete_remote`

**Depends on:** Stage 1

- [ ] 3.1 Implement `internal/verbs/branch_delete_remote.go`: `func BranchDeleteRemote(ctx context.Context, in BranchDeleteRemoteInput) (*BranchDeleteRemoteResult, error)`
- [ ] 3.2 Input validation: refuse empty `branch`
- [ ] 3.3 Source-repo resolution + allowlist + lock
- [ ] 3.4 Resolve default branch via `git symbolic-ref refs/remotes/origin/HEAD`; refuse if `branch` matches
- [ ] 3.5 Pre-check branch exists on origin via `git ls-remote --heads origin <branch>`; refuse on miss
- [ ] 3.6 Capture `prior_remote_sha` from the ls-remote output
- [ ] 3.7 Shell out: `git push origin :<branch>`
- [ ] 3.8 Return `{ deleted: true, branch, prior_remote_sha }`
- [ ] 3.9 Verify Stage 1.2 tests pass

## 4. `iris:tag`

**Depends on:** Stage 1

- [ ] 4.1 Implement `internal/verbs/tag.go`: `func Tag(ctx context.Context, in TagInput) (*TagResult, error)`
- [ ] 4.2 Input validation: refuse empty `tag`; default `message` to "Released by iris"
- [ ] 4.3 Source-repo resolution + allowlist + lock
- [ ] 4.4 Conflict check: `git rev-parse <tag>` (local) and `git ls-remote --tags origin <tag>` (remote). Refuse if either hits
- [ ] 4.5 Resolve `origin/<default-branch>` SHA for the tag target
- [ ] 4.6 Shell out: `git tag -a <tag> -m <message> <target-sha>` then `git push origin <tag>`
- [ ] 4.7 Return `{ tagged: true, tag, sha, message }`
- [ ] 4.8 Verify Stage 1.3 tests pass

## 5. MCP handlers, Cobra subcommands, daemon registration

**Depends on:** Stages 2, 3, 4

- [ ] 5.1 Implement `internal/mcp/handler_fetch.go`, `handler_branch_delete_remote.go`, `handler_tag.go` following the existing handler pattern
- [ ] 5.2 Implement `cmd/iris/fetch.go`, `branch_delete_remote.go`, `tag.go` following the existing direct-CLI pattern
- [ ] 5.3 Register the 3 new tools in `internal/daemon/run.go`'s `toolDefinitions()`
- [ ] 5.4 Update `README.md` CLI section
- [ ] 5.5 Run `make test` under `-race`; verify all stages pass
- [ ] 5.6 Run `openspec validate add-source-repo-utility-verbs --strict`
