## 1. Shared scaffolding (already in place pre-agent-fanout)

- [x] 1.1 OpenSpec change folder created with proposal.md, design.md, tasks.md, and delta spec.md stubs.

## 2. Status enrichment + warning silencing (Agent A)

- [x] 2.1 Add `ListTasks` method to `internal/argus/Client` (GET `/api/tasks`, return `[]Task`).
- [x] 2.2 Add `FindTaskByWorktreePath` helper in `internal/verbs/` (or near Resolve) that calls ListTasks and filters using `EqualSourceRepos`.
- [x] 2.3 Add `Branch` (string) and `ArgusTask` (*argus.Task) fields to `verbs.StatusResult`.
- [x] 2.4 Populate `Branch` from `git rev-parse --abbrev-ref HEAD` in the resolved source repo.
- [x] 2.5 Populate `ArgusTask` when iris can resolve a task matching the source repo; null otherwise. Do not fail Status if argus is unreachable – warn and leave null.
- [x] 2.6 Change `LoadIrisToml`: ENOENT returns `(nil, nil, nil)` instead of synthesizing a ValidationError. Update existing callers that depend on the old shape (search for `LoadIrisToml`).
- [x] 2.7 Update `verbs.Status` to set `Config: nil` with no warning when `doc == nil` (file absent). Parse errors still flow as warnings.
- [x] 2.8 Update `internal/verbs/status_test.go` for the new fields + silent-missing-config behavior.
- [x] 2.9 Update other tests broken by the LoadIrisToml signature/behavior change.
- [x] 2.10 Update tool description for `iris_status` in `internal/daemon/run.go` to mention the new fields.
- [x] 2.11 `make test`, `make vet`, `gofmt -l .` clean.
- [x] 2.12 Update this change's `specs/iris-status/spec.md` delta with the final field shape and scenarios.

## 3. Merge enhancements + post_merge hook (Agent B)

- [x] 3.1 Add `PostMerge *HookBlock` to `config.IrisToml` (toml tag `post_merge`).
- [x] 3.2 Wire `PostMerge` into `IrisToml.Validate` (reuse existing HookBlock validate with blockName="post_merge").
- [x] 3.3 Add `iris_toml_test.go` cases for the new `[post_merge]` block (happy + missing command + bad working_directory).
- [x] 3.4 Add `DryRun bool` to `verbs.MergeOptions`; add `TaskBranchStillExists`, `WorktreeStillPresent`, `PostMerge *PostMergeResult`, `DryRun bool`, `WouldSucceed bool`, `FilesChanged []string`, `Conflicts []string` to `MergeResult` (group dry-run-only fields together; keep struct readable).
- [x] 3.5 Implement dry-run path: `fetch + checkout default + pull --ff-only + merge --no-commit --no-ff <branch>`, capture `git diff --cached --name-only` (or equivalent) and conflict list from `git diff --name-only --diff-filter=U`, then `merge --abort` unconditionally. Set `DryRun: true`, `WouldSucceed`, `FilesChanged`, `Conflicts`. No post_merge.
- [x] 3.6 Implement post_merge execution: after a successful real merge, if `cfg.PostMerge != nil`, run the command with env IRIS_TASK_ID, IRIS_TASK_BRANCH, IRIS_SOURCE_REPO, IRIS_DEFAULT_BRANCH, IRIS_MERGE_SHA. Honor working_directory (relative to source repo) and timeout_seconds (default 60). Capture stdout/stderr/exit_code/duration into `PostMergeResult`.
- [x] 3.7 A non-zero post_merge exit reports failure in the result but does NOT roll back the merge. Caller is informed via `post_merge.exit_code` and `post_merge.error`.
- [x] 3.8 Set `TaskBranchStillExists: true` and `WorktreeStillPresent: true` on every successful return path (real and dry-run). The fields are factual descriptions of iris's output state.
- [x] 3.9 Update `internal/verbs/merge_to_master_test.go` for: dry-run happy + dry-run conflict + post_merge success + post_merge failure (non-rollback) + post_merge env vars + postconditions present on every success.
- [x] 3.10 Update `internal/mcp/handler_merge_to_master.go` to accept `dry_run` from input.
- [x] 3.11 Update `cmd/iris/merge_to_master.go` to add `--dry-run` flag.
- [x] 3.12 Update tool description in `internal/daemon/run.go` for `iris_merge_to_master`: clarify that the verb does NOT delete the task branch or the worktree, reference `iris_complete_task` and `iris_branch_delete_remote` as follow-ups, document `dry_run`.
- [x] 3.13 Update README with `--dry-run`, the `[post_merge]` block, and the new result fields.
- [x] 3.14 `make test`, `make vet`, `gofmt -l .` clean.
- [x] 3.15 Update this change's `specs/iris-merge-to-master/spec.md` delta with the final field shape and scenarios.

## 4. Integration validation (after both agents merge back)

- [ ] 4.1 Merge Agent A's branch and Agent B's branch into argus/iris-feedback-rough-edges. Resolve conflicts in `iris_toml.go`, `daemon/run.go` if any.
- [ ] 4.2 `make test -race -count=1` green on the merged branch.
- [ ] 4.3 `go vet ./...` clean.
- [ ] 4.4 `openspec validate consumer-ergonomics --strict` clean.
- [ ] 4.5 `openspec validate --all --strict` clean.
