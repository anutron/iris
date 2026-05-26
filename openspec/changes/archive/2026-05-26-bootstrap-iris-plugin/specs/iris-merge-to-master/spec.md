## ADDED Requirements

### Requirement: `iris:merge_to_master` verb

The plugin SHALL expose `iris:merge_to_master` as an MCP tool accepting `task_id` (string, required), `no_ff` (bool, default true), and `message` (string, optional). On success the verb SHALL return the merge SHA and the merge log; on failure it SHALL return a structured error and leave the source repo on its original branch.

#### Scenario: Successful merge

- **WHEN** the verb is invoked for a task whose source repo is clean and whose branch `argus/<task-slug>` is ahead of master
- **THEN** iris runs `git fetch --all --prune`, `git checkout master`, `git pull --ff-only`, `git merge --no-ff argus/<task-slug>` in the source repo and returns `{ok: true, sha: "<merge-sha>", log: "<git output>"}`

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

- **WHEN** the user runs `iris merge-to-master <task-id> [--no-ff] [-m MESSAGE]` from any shell on the host
- **THEN** the same `verbs.MergeToMaster` Go function executes (bypassing the daemon process) and prints the structured result

#### Scenario: Refuses to merge when origin/HEAD is not set

- **WHEN** the verb is invoked and the source repo's `git symbolic-ref refs/remotes/origin/HEAD` is unset
- **THEN** iris returns a structured error naming the source repo and the `git remote set-head origin --auto` command the operator must run, and performs no git mutation
