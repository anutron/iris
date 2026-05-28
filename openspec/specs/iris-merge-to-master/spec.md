# iris-merge-to-master Specification

## Purpose
TBD - created by archiving change bootstrap-iris-plugin. Update Purpose after archive.
## Requirements
### Requirement: `iris:merge_to_master` verb

The plugin SHALL expose `iris:merge_to_master` as an MCP tool accepting `task_id` (string, required), `no_ff` (bool, default true), `message` (string, optional), and `dry_run` (bool, default false). On success the verb SHALL return the merge SHA, the merge log, factual postconditions about iris's output state (`task_branch_still_exists`, `worktree_still_present`), and the post_merge hook outcome when configured; on failure it SHALL return a structured error and leave the source repo on its original branch.

The result shape adds:

- `task_branch_still_exists` (bool) – true on every successful return path. Iris does not delete the branch.
- `worktree_still_present` (bool) – true on every successful return path. Iris does not delete the worktree.
- `post_merge` (object or null) – populated when `.iris.toml` declares a `[post_merge]` block and the merge was not a dry run. Shape: `{ exit_code, stdout, stderr, duration_ms, error }`. `error` is non-empty when iris could not execute the hook (e.g., binary missing, timeout); `exit_code` is `-1` when the hook could not be executed (timeout or exec failure), the process's real exit code otherwise. Null when no hook was configured or when `dry_run: true`. The hook also runs when the merge step is invoked indirectly via `iris:complete_task`.

Dry-run mode SHALL:

- Hold the same per-source-repo lock as a real merge.
- Run `fetch + checkout default + pull --ff-only + merge --no-commit --no-ff <branch>`, capture the would-be state, then `merge --abort` unconditionally.
- Return `dry_run: true`, `would_succeed: bool`, `files_changed: []string`, `conflicts: []string`. The `sha` field is empty.
- NOT execute the `[post_merge]` hook.

#### Scenario: Successful merge

- **WHEN** the verb is invoked for a task whose source repo is clean and whose branch `argus/<task-slug>` is ahead of master
- **THEN** iris runs `git fetch --all --prune`, `git checkout master`, `git pull --ff-only`, `git merge --no-ff argus/<task-slug>` in the source repo and returns `{ok: true, sha: "<merge-sha>", log: "<git output>", task_branch_still_exists: true, worktree_still_present: true, post_merge: null}` when no post_merge hook is configured

#### Scenario: Postconditions present on every success path

- **WHEN** the verb returns a success result (real merge or dry run)
- **THEN** the result includes `task_branch_still_exists: true` and `worktree_still_present: true` describing iris's output state

#### Scenario: Refuses to merge non-argus branch

- **WHEN** the verb is invoked for a task whose branch name is not `argus/<task-slug>` (e.g., the task record points at `main` or a random branch)
- **THEN** iris returns a structured error and performs no git mutation

#### Scenario: Refuses to merge master into master

- **WHEN** the resolved task branch equals `master` (or `main`, whichever is the default branch)
- **THEN** iris returns a structured error and performs no git mutation

#### Scenario: Merge conflict aborts cleanly

- **WHEN** `git merge --no-ff argus/<task-slug>` exits non-zero with a conflict
- **THEN** iris runs `git merge --abort`, leaves the source repo on master with no in-progress merge, and returns a structured error listing the conflicting paths

#### Scenario: `no_ff=false` allows fast-forward

- **WHEN** the verb is invoked with `no_ff=false` and the task branch is a strict descendant of master
- **THEN** iris runs `git merge --ff-only argus/<task-slug>` and returns the new head SHA

#### Scenario: Custom merge message

- **WHEN** the verb is invoked with a non-empty `message`
- **THEN** iris passes `-m "<message>"` to `git merge` and the resulting commit uses that subject line

#### Scenario: Direct CLI invocation runs the same verb

- **WHEN** the user runs `iris merge-to-master <task-id> [--no-ff] [-m MESSAGE] [--dry-run]` from any shell on the host
- **THEN** the same `verbs.MergeToMaster` Go function executes (bypassing the daemon process) and prints the structured result

#### Scenario: Refuses to merge when origin/HEAD is not set

- **WHEN** the verb is invoked and the source repo's `git symbolic-ref refs/remotes/origin/HEAD` is unset
- **THEN** iris returns a structured error naming the source repo and the `git remote set-head origin --auto` command the operator must run, and performs no git mutation

#### Scenario: Dry-run previews a clean merge

- **GIVEN** a task whose branch would merge cleanly into the default branch
- **WHEN** the verb is invoked with `dry_run: true`
- **THEN** iris attempts `git merge --no-commit --no-ff <branch>`, captures the file list and (empty) conflict list, runs `git merge --abort`, and returns `{dry_run: true, would_succeed: true, files_changed: [...], conflicts: [], sha: ""}`. The source repo is on the default branch with no in-progress merge.

#### Scenario: Dry-run previews a conflicted merge

- **GIVEN** a task whose branch conflicts with the default branch
- **WHEN** the verb is invoked with `dry_run: true`
- **THEN** iris captures the conflict list, runs `git merge --abort`, and returns `{dry_run: true, would_succeed: false, files_changed: [...], conflicts: [<conflicted paths>], sha: ""}` with no error

#### Scenario: Dry-run skips post_merge hook

- **GIVEN** a `.iris.toml` with a `[post_merge]` block
- **WHEN** the verb is invoked with `dry_run: true`
- **THEN** iris does NOT execute the post_merge command and returns `post_merge: null`

#### Scenario: post_merge hook runs after successful merge

- **GIVEN** a `.iris.toml` declaring `[post_merge] command = ["echo", "done"]`
- **WHEN** the verb successfully merges
- **THEN** iris executes the command with env `IRIS_TASK_ID`, `IRIS_TASK_BRANCH`, `IRIS_SOURCE_REPO`, `IRIS_DEFAULT_BRANCH`, `IRIS_MERGE_SHA` set, captures stdout/stderr/exit_code/duration_ms, and returns them in `post_merge`

#### Scenario: post_merge env vars carry merge context

- **GIVEN** a `[post_merge]` hook
- **WHEN** the hook executes
- **THEN** the process environment includes `IRIS_TASK_ID=<task>`, `IRIS_TASK_BRANCH=<branch>`, `IRIS_SOURCE_REPO=<absolute path>`, `IRIS_DEFAULT_BRANCH=<main|master>`, `IRIS_MERGE_SHA=<new HEAD>`

#### Scenario: post_merge failure does not roll back the merge

- **GIVEN** a `[post_merge]` hook that exits non-zero
- **WHEN** the verb completes the merge then runs the hook
- **THEN** the merge remains on the default branch, the result includes `post_merge.exit_code != 0` and any captured stderr, and the verb returns success (the merge succeeded; the hook is informational)

#### Scenario: post_merge timeout terminates the hook

- **GIVEN** a `[post_merge]` hook with `timeout_seconds = 1` running a command that sleeps longer
- **WHEN** the timeout fires
- **THEN** iris kills the process, returns `post_merge.error: "timeout after 1s"` and a non-zero exit_code (or a sentinel), and reports the merge as successful

#### Scenario: post_merge respects working_directory

- **GIVEN** a `[post_merge]` block with `working_directory = "subdir"`
- **WHEN** the hook executes
- **THEN** iris runs the command with cwd = `<source-repo>/subdir`

#### Scenario: Missing `.iris.toml` does not block merge

- **GIVEN** a source repo with no `.iris.toml`
- **WHEN** the verb is invoked
- **THEN** iris merges successfully and returns `post_merge: null`

