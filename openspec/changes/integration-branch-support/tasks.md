**Design doc:** `openspec/changes/integration-branch-support/design.md`

## 1. Tests: iris:merge_to_branch (Part A)

- [x] 1.1 Add failing tests in `internal/verbs/merge_to_branch_test.go`: guard cases — empty `target_branch`/`source_ref`, leading-dash `target_branch`/`source_ref`, `target_branch == source_ref`, `target_branch` equal to default/`main`/`master`
- [x] 1.2 Add failing test: happy-path merge into a non-default `target_branch` from an arbitrary `source_ref`, asserting the merge lands on `origin/target_branch` and `pushed: true`
- [x] 1.3 Add failing test: source repo's checked-out branch and HEAD SHA are unchanged before/after the call
- [x] 1.4 Add failing test: the scratch worktree is removed after the call (`git worktree list` has no leftover entry) on both success and failure paths
- [x] 1.5 Add failing test: `target_branch` whose local ref is stale relative to `origin/target_branch` is reconciled (reset to origin) before merging, so the push succeeds
- [x] 1.6 Add failing test: `target_branch` that exists only locally (never pushed) merges from local state and the push creates the branch on origin
- [x] 1.7 Add failing test: merge conflict aborts cleanly — no push, worktree removed, `origin/target_branch` unchanged, structured error returned
- [x] 1.8 Add failing test: `no_ff=false` performs a fast-forward merge
- [x] 1.9 Add failing test: custom `message` is used as the merge commit subject
- [x] 1.10 Add failing test: `dry_run=true` previews a clean merge (`would_succeed: true`, `files_changed`, empty `conflicts`, empty `sha`, no push)
- [x] 1.11 Add failing test: `dry_run=true` previews a conflicted merge (`would_succeed: false`, `conflicts` populated)
- [x] 1.12 Add failing test: `dry_run=true` skips the post_merge hook and the push
- [x] 1.13 Add failing test: post_merge hook runs after a successful merge+push, reading `.iris.toml` from `target_branch`'s tree (not the source repo's current checkout), with `IRIS_TASK_ID`/`IRIS_SOURCE_REPO`/`IRIS_TARGET_BRANCH`/`IRIS_SOURCE_REF`/`IRIS_MERGE_SHA` set
- [x] 1.14 Add failing test: post_merge hook failure (non-zero exit) does not roll back the merge/push
- [x] 1.15 Add failing test: missing `.iris.toml` on `target_branch` does not block the merge (`post_merge: null`)
- [x] 1.16 Add failing test: an arbitrary `source_ref` (tag or raw SHA, not a branch) merges successfully
- [x] 1.17 Add failing test: unknown `task_id` and non-allowlisted source repo are refused with no git mutation (reuse `stubArgusTaskNotFound` / allowlist patterns from `merge_to_master_test.go`)
- [x] 1.18 Confirm every acceptance criterion in `design.md` / the `iris-merge-to-branch` delta spec maps to a test (Prove-It Pattern)

## 2. Implement iris:merge_to_branch (Part A)

**Depends on:** Stage 1

- [x] 2.1 Create `internal/verbs/merge_to_branch.go`: `MergeToBranchResult` type; `MergeToBranch(ctx, client, taskID, targetBranch, sourceRef string, opts MergeOptions) (*MergeToBranchResult, error)` reusing `verbs.MergeOptions` (NoFF/Message/DryRun)
- [x] 2.2 Implement `guardMergeToBranch(targetBranch, sourceRef, defaultBranch string) error` (empty checks, leading-dash checks, self-merge refusal, default/protected-branch refusal)
- [x] 2.3 Implement `setupScratchWorktree(ctx, sourceRepo, targetBranch string) (worktreePath string, cleanup func(), err error)`: fetch --all --prune, `os.MkdirTemp`, `git worktree add`, conditional `git reset --hard origin/<target_branch>`, cleanup closure (`git worktree remove --force`, fallback `os.RemoveAll` + `worktree prune`)
- [x] 2.4 Implement `mergeToBranchLocked(...)`: merge (no-ff/ff-only/message), deferred abort-on-cancel (mirrors `mergeToMasterLocked`), push `target_branch` to origin, invoke post_merge hook on success
- [x] 2.5 Implement `dryRunMergeToBranchLocked(...)`: `--no-commit --no-ff` merge in the scratch worktree, capture files_changed/conflicts via the existing `listGitPaths` helper, abort, no push, no hook
- [x] 2.6 Implement `runMergeToBranchPostMergeHook(ctx, resolved, worktreePath, targetBranch, sourceRef, mergeSHA)`: loads `.iris.toml` from `worktreePath` (not `resolved.SourceRepo`), runs `[post_merge]` with `IRIS_TARGET_BRANCH`/`IRIS_SOURCE_REF` env vars
- [x] 2.7 Run the stage-1 `merge_to_branch_test.go` tests; confirm green

## 3. Wire iris:merge_to_branch (Part A)

**Depends on:** Stage 2

- [x] 3.1 Add `internal/mcp/handler_merge_to_branch.go`: decode `task_id`/`target_branch`/`source_ref`/`no_ff`/`message`/`dry_run`, call `verbs.MergeToBranch`
- [x] 3.2 Add `cmd/iris/merge_to_branch.go`: `iris merge-to-branch <task-id> <target-branch> <source-ref> [--no-ff] [-m MESSAGE] [--dry-run]`, register in `cmd/iris/main.go`
- [x] 3.3 Register `mcpSrv.RegisterHandler("iris_merge_to_branch", mcp.NewMergeToBranchHandler(client))` in `internal/daemon/run.go` (near L72-96)
- [x] 3.4 Add the `iris_merge_to_branch` tool schema to `toolDefinitions()` in `internal/daemon/run.go` (near L190+)

## 4. Tests: iris:gh_pr_create base param (Part B)

**Depends on:** Stage 1 (independent of Part A implementation; may run in parallel)

- [x] 4.1 Add failing test in `internal/verbs/gh_pr_create_test.go`: `base` overrides the target branch in same-repo-on-origin mode
- [x] 4.2 Add failing test in `internal/verbs/gh_pr_create_crossfork_test.go` (or equivalent): `base` composes with `base_repo` mode (`--repo <base_repo> --base <base> --head <effective>`)
- [x] 4.3 Add failing test: `base` composes with cross-fork auto-detection mode (`--repo <upstream> --base <base> --head <fork-owner>:<effective>`)
- [x] 4.4 Add failing test: omitted `base` preserves each mode's existing default-branch behavior unchanged
- [x] 4.5 Add failing test: a `base` beginning with `-` is rejected before gh runs
- [x] 4.6 Confirm every acceptance criterion in the `iris-gh-pr-create` delta spec maps to a test

## 5. Implement iris:gh_pr_create base param (Part B)

**Depends on:** Stage 4

- [x] 5.1 Add `Base string` to `GHPRCreateOptions` in `internal/verbs/gh_pr_create.go`; validate leading-dash before the resolve/gh call
- [x] 5.2 Compose `base` into all three target-mode branches (`base_repo`, cross-fork, same-repo-on-origin) per the delta spec
- [x] 5.3 Run the stage-4 `base` tests; confirm green

## 6. Wire iris:gh_pr_create base param (Part B)

**Depends on:** Stage 5

- [x] 6.1 Add `base` (string, optional) to `ghPRCreateInput` in `internal/mcp/handler_gh_pr_create.go` and pass into `GHPRCreateOptions`
- [x] 6.2 Add `--base` flag to `newGHPRCreateCmd` in `cmd/iris/gh_pr_create.go`
- [x] 6.3 Update the `iris_gh_pr_create` MCP tool schema + description in `internal/daemon/run.go` to advertise `base`

## 7. Docs and verification

**Depends on:** Stage 3, Stage 6

- [x] 7.1 Update `README.md` CLI table (`iris merge-to-branch`, `--base` on `gh-pr-create`) and add a short integration-branch workflow note
- [x] 7.2 Run the full `go build ./...`, `go vet ./...`, and `go test ./...`; confirm green
- [x] 7.3 Run `gofmt -l` on changed files; confirm clean
- [x] 7.4 Run `openspec validate integration-branch-support --strict` (and `openspec validate --all --strict`); confirm clean
