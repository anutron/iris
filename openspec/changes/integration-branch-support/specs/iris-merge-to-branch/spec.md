## ADDED Requirements

### Requirement: `iris:merge_to_branch` verb

The plugin SHALL expose `iris:merge_to_branch` as an MCP tool accepting `task_id` (string, required), `target_branch` (string, required), `source_ref` (string, required), `no_ff` (bool, default true), `message` (string, optional), and `dry_run` (bool, default false). The verb SHALL merge (never cherry-pick) `source_ref` into `target_branch` and push `target_branch` to `origin`, performing the merge in a scratch `git worktree add` created in a temporary directory that is removed on exit, so that the source repo's currently checked-out branch and working tree are never changed. `task_id` SHALL be used only to resolve and allowlist-check the source repo (per `Resolve`); `target_branch` and `source_ref` SHALL NOT be constrained to any branch-naming prefix (unlike `iris:merge_to_master`'s `argus/`-prefix restriction on its source).

On success the verb SHALL return the merge SHA, `target_branch`, `source_ref`, the source repo path, a `pushed` boolean, the git log, and the post_merge hook outcome when configured. On failure it SHALL return a structured error, perform no push, and leave both the source repo's checkout and `origin/<target_branch>` unchanged.

#### Scenario: Successful merge into a long-lived integration branch

- **WHEN** the verb is invoked with `target_branch="integration/big-feature"` and `source_ref="feature-x"`, both resolvable in the source repo
- **THEN** iris creates a scratch worktree checked out at `integration/big-feature`, runs `git merge --no-ff feature-x` there, pushes the result to `origin/integration/big-feature`, removes the scratch worktree, and returns `{sha, target_branch: "integration/big-feature", source_ref: "feature-x", pushed: true, ...}`

#### Scenario: Source repo's checkout is never disturbed

- **GIVEN** the source repo is currently checked out on some branch `X` (different from `target_branch`)
- **WHEN** `iris:merge_to_branch` runs (success or failure)
- **THEN** the source repo's checked-out branch and HEAD SHA are identical before and after the call

#### Scenario: Scratch worktree is always cleaned up

- **WHEN** `iris:merge_to_branch` completes, whether it succeeds, hits a merge conflict, or is refused by a guard after the worktree was created
- **THEN** the temporary worktree directory no longer exists and `git worktree list` in the source repo shows no leftover entry for it

#### Scenario: Arbitrary source_ref types are accepted

- **WHEN** the verb is invoked with `source_ref` set to a tag name or a raw commit SHA (not a branch)
- **THEN** iris merges that ref into `target_branch` exactly as it would a branch name

#### Scenario: target_branch not tracking origin is reconciled before merging

- **GIVEN** the source repo's local ref for `target_branch` is behind `origin/target_branch` (e.g. another actor pushed directly to origin without updating this repo's local ref)
- **WHEN** the verb is invoked
- **THEN** iris fetches, resets the scratch worktree to `origin/target_branch`, merges `source_ref` on top of that up-to-date state, and the push succeeds (a merge based on the stale local ref would instead be rejected as non-fast-forward)

#### Scenario: target_branch not yet on origin is merged from local state

- **GIVEN** `target_branch` exists only as a local branch in the source repo, never pushed
- **WHEN** the verb is invoked
- **THEN** iris skips the reset-to-origin step, merges `source_ref` into the local branch tip, and the push creates `origin/target_branch`

#### Scenario: Refuses empty target_branch or source_ref

- **WHEN** the verb is invoked with an empty `target_branch` or an empty `source_ref`
- **THEN** iris returns a structured error before creating any worktree or invoking git, and performs no mutation

#### Scenario: Refuses a target_branch or source_ref beginning with a dash

- **WHEN** the verb is invoked with `target_branch` or `source_ref` beginning with `-` (e.g. `--upload-pack=evil`)
- **THEN** iris returns a structured error stating the argument must not begin with `-`, does NOT invoke git, and performs no mutation

#### Scenario: Refuses merging a branch into itself

- **WHEN** `target_branch` and `source_ref` are the same branch name
- **THEN** iris returns a structured error and performs no mutation

#### Scenario: Refuses targeting the default or protected branch

- **WHEN** `target_branch` equals the source repo's resolved default branch, or is literally `main` or `master`
- **THEN** iris returns a structured error directing the caller to `iris:merge_to_master`, and performs no mutation

#### Scenario: Merge conflict aborts cleanly

- **WHEN** merging `source_ref` into `target_branch` in the scratch worktree produces a conflict
- **THEN** iris runs `git merge --abort` in the scratch worktree, removes the worktree, performs no push, leaves `origin/target_branch` unchanged, and returns a structured error

#### Scenario: `no_ff=false` allows fast-forward

- **WHEN** the verb is invoked with `no_ff=false` and `source_ref` is a strict descendant of `target_branch`
- **THEN** iris runs `git merge --ff-only source_ref` in the scratch worktree and returns the new head SHA

#### Scenario: Custom merge message

- **WHEN** the verb is invoked with a non-empty `message`
- **THEN** iris passes `-m "<message>"` to `git merge` and the resulting merge commit uses that subject line

#### Scenario: Dry-run previews a clean merge

- **GIVEN** `source_ref` would merge cleanly into `target_branch`
- **WHEN** the verb is invoked with `dry_run: true`
- **THEN** iris creates the scratch worktree, runs `git merge --no-commit --no-ff source_ref`, captures the file list and (empty) conflict list, runs `git merge --abort`, removes the worktree, performs no push, and returns `{dry_run: true, would_succeed: true, files_changed: [...], conflicts: [], sha: ""}`

#### Scenario: Dry-run previews a conflicted merge

- **GIVEN** `source_ref` conflicts with `target_branch`
- **WHEN** the verb is invoked with `dry_run: true`
- **THEN** iris captures the conflicting paths, aborts, removes the worktree, performs no push, and returns `{dry_run: true, would_succeed: false, files_changed: [...], conflicts: [<paths>], sha: ""}` with no error

#### Scenario: Dry-run skips post_merge hook and push

- **GIVEN** a `.iris.toml` on `target_branch` declaring a `[post_merge]` block
- **WHEN** the verb is invoked with `dry_run: true`
- **THEN** iris does NOT execute the post_merge command, does NOT push, and returns `post_merge: null`

#### Scenario: post_merge hook runs from the merged branch's tree after a successful push

- **GIVEN** a `.iris.toml` on `target_branch` declaring `[post_merge] command = ["echo", "done"]`
- **WHEN** the verb successfully merges and pushes
- **THEN** iris executes the command with the scratch worktree (now at the merge commit) as its working directory, with env `IRIS_TASK_ID`, `IRIS_SOURCE_REPO`, `IRIS_TARGET_BRANCH`, `IRIS_SOURCE_REF`, `IRIS_MERGE_SHA` set, captures stdout/stderr/exit_code/duration_ms, and returns them in `post_merge`

#### Scenario: post_merge hook reads .iris.toml from the merged tree, not the source repo's current checkout

- **GIVEN** the source repo's currently checked-out branch has no `.iris.toml` (or a different one) but `target_branch` declares a `[post_merge]` hook
- **WHEN** the verb successfully merges and pushes
- **THEN** iris runs the hook declared on `target_branch`, not whatever (or nothing) is configured on the source repo's untouched checkout

#### Scenario: post_merge failure does not roll back the merge or push

- **GIVEN** a `[post_merge]` hook that exits non-zero
- **WHEN** the verb completes the merge and push, then runs the hook
- **THEN** `origin/target_branch` retains the merge, the result includes `post_merge.exit_code != 0` and any captured stderr, and the verb returns success

#### Scenario: Missing .iris.toml does not block merge

- **GIVEN** `target_branch`'s tree has no `.iris.toml`
- **WHEN** the verb is invoked
- **THEN** iris merges and pushes successfully and returns `post_merge: null`

#### Scenario: Refuses an unknown task ID

- **WHEN** the verb is invoked with a `task_id` argus does not recognize
- **THEN** iris returns a structured error naming the task ID and performs no git mutation

#### Scenario: Refuses a source repo outside the project allowlist

- **WHEN** the resolved source-repo path does not match any allowlisted argus project
- **THEN** iris returns a structured error naming the rejected path and performs no git mutation

#### Scenario: Direct CLI invocation runs the same verb

- **WHEN** the user runs `iris merge-to-branch <task-id> <target-branch> <source-ref> [--no-ff] [-m MESSAGE] [--dry-run]` from any shell on the host
- **THEN** the same `verbs.MergeToBranch` Go function executes (bypassing the daemon process) and prints the structured result
