# iris-checkout Specification

## ADDED Requirements

### Requirement: `iris:checkout` verb

The plugin SHALL expose `iris:checkout` as an MCP tool and CLI subcommand that switches the resolved source repo to a branch. The verb SHALL accept `task_id` (required, string), `branch` (required, non-empty string), and `force` (optional, boolean, default false). On success, the verb SHALL return `{ checked_out: true, branch, head_sha, prior_branch, prior_head }`.

When `force` is true, the verb SHALL run a best-effort recovery sequence (`git merge --abort`, `git cherry-pick --abort`, `git rebase --abort`) before `git checkout -f <branch>`, so that a source repo stuck mid-merge or mid-cherry-pick with a dirty working tree can be returned to a clean state on `branch`.

#### Scenario: Plain checkout succeeds on a clean working tree

- **WHEN** `iris:checkout` is invoked with `force=false` and the working tree is clean and no merge/cherry-pick is in progress
- **THEN** iris runs `git checkout <branch>` and returns `{ checked_out: true, branch, head_sha, prior_branch, prior_head }`

#### Scenario: Refuses empty branch

- **WHEN** invoked with an empty `branch`
- **THEN** iris returns a structured error naming the field and does NOT shell out to git

#### Scenario: Refuses leading-dash branch (flag-smuggling guard)

- **WHEN** invoked with `branch` that starts with `-`
- **THEN** iris returns an `invalid branch name` error and does NOT shell out to git

#### Scenario: Refuses unknown branch

- **WHEN** `branch` does not exist locally and no `origin/<branch>` is available to auto-track
- **THEN** iris returns a structured error carrying git's stderr and does NOT change the source repo's checkout

#### Scenario: Propagates git's refusal when force=false and working tree dirty

- **WHEN** `force=false` and the source repo has unstaged changes that would be overwritten by the switch
- **THEN** iris returns a structured error carrying git's stderr and does NOT change the source repo's checkout

#### Scenario: Propagates git's refusal when force=false and a merge/cherry-pick is in progress

- **WHEN** `force=false` and the source repo has `MERGE_HEAD` or `CHERRY_PICK_HEAD` set
- **THEN** iris returns a structured error carrying git's stderr and does NOT change the source repo's checkout

#### Scenario: force=true recovers from in-progress merge

- **WHEN** `force=true` and the source repo has `MERGE_HEAD` set (mid-merge with conflicts)
- **THEN** iris runs `git merge --abort` (best-effort), then `git checkout -f <branch>`, and returns success with `prior_branch` and `prior_head` populated from the state observed before the recovery

#### Scenario: force=true recovers from in-progress cherry-pick

- **WHEN** `force=true` and the source repo has `CHERRY_PICK_HEAD` set
- **THEN** iris runs `git cherry-pick --abort` (best-effort), then `git checkout -f <branch>`, and returns success

#### Scenario: force=true discards uncommitted changes

- **WHEN** `force=true` and the working tree has uncommitted modifications
- **THEN** iris runs `git checkout -f <branch>`, the modifications are discarded, and iris returns success

#### Scenario: prior_branch and prior_head reflect pre-call state

- **WHEN** `iris:checkout` is invoked and succeeds
- **THEN** `prior_branch` matches the branch the source repo was on before the call (or empty if detached HEAD), and `prior_head` matches the SHA HEAD pointed to before the call

#### Scenario: Refuses unknown task ID

- **WHEN** invoked with an unknown `task_id`
- **THEN** iris returns a structured error and does NOT shell out to git

#### Scenario: Refuses non-allowlisted source repo

- **WHEN** the resolved source repo is not on the argus project allowlist
- **THEN** iris returns a structured error naming the rejected path

#### Scenario: Per-source-repo lock held for checkout

- **WHEN** two concurrent `iris:checkout` calls target the same source repo
- **THEN** the second blocks until the first releases the lock

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris checkout <task-id> <branch> [--force]`
- **THEN** the same `verbs.Checkout` Go function executes and prints the structured result
