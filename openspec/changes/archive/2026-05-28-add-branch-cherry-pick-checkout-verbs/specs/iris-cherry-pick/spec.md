# iris-cherry-pick Specification

## ADDED Requirements

### Requirement: `iris:cherry_pick` verb

The plugin SHALL expose `iris:cherry_pick` as an MCP tool and CLI subcommand that cherry-picks a commit onto a target branch in the resolved source repo. The verb SHALL accept `task_id` (required, string), `commit` (required, non-empty string), and `target_branch` (required, non-empty string). On success, the verb SHALL return `{ cherry_picked: true, commit, target_branch, new_sha }` and SHALL leave the source repo on `target_branch`.

#### Scenario: Successful cherry-pick returns new SHA

- **WHEN** `iris:cherry_pick` is invoked with a `commit` that applies cleanly to `target_branch`
- **THEN** iris checks out `target_branch`, runs `git cherry-pick <commit>`, and returns `{ cherry_picked: true, commit, target_branch, new_sha }` where `new_sha` is the HEAD of `target_branch` after the pick

#### Scenario: Source repo lands on target_branch after success

- **WHEN** `iris:cherry_pick` succeeds
- **THEN** the source repo's HEAD points to `target_branch`

#### Scenario: Refuses empty commit

- **WHEN** invoked with an empty `commit`
- **THEN** iris returns a structured error naming the field and does NOT shell out to git

#### Scenario: Refuses empty target_branch

- **WHEN** invoked with an empty `target_branch`
- **THEN** iris returns a structured error naming the field and does NOT shell out to git

#### Scenario: Refuses leading-dash commit (flag-smuggling guard)

- **WHEN** invoked with `commit` that starts with `-`
- **THEN** iris returns an `invalid commit` error and does NOT shell out to git

#### Scenario: Refuses leading-dash target_branch (flag-smuggling guard)

- **WHEN** invoked with `target_branch` that starts with `-`
- **THEN** iris returns an `invalid target_branch` error and does NOT shell out to git

#### Scenario: Refuses cherry-pick onto default branch

- **WHEN** `target_branch` equals the resolved default branch (or `main` or `master`)
- **THEN** iris returns a structured error naming the protected branch

#### Scenario: Refuses unknown target_branch

- **WHEN** `target_branch` does not exist locally
- **THEN** iris returns a structured error carrying git's stderr and does NOT change the source repo's checkout

#### Scenario: Refuses unresolvable commit

- **WHEN** `commit` does not resolve to a commit object
- **THEN** iris returns a structured error carrying git's stderr and does NOT change the source repo's checkout

#### Scenario: Aborts cleanly on conflict

- **WHEN** `git cherry-pick` produces a conflict
- **THEN** iris captures conflict paths from `git diff --name-only --diff-filter=U`, runs `git cherry-pick --abort`, and returns a structured error carrying the conflict paths
- **AND** the source repo's working tree is clean and HEAD points to `target_branch`

#### Scenario: Refuses unknown task ID

- **WHEN** invoked with an unknown `task_id`
- **THEN** iris returns a structured error and does NOT shell out to git

#### Scenario: Refuses non-allowlisted source repo

- **WHEN** the resolved source repo is not on the argus project allowlist
- **THEN** iris returns a structured error naming the rejected path

#### Scenario: Per-source-repo lock held for cherry-pick

- **WHEN** two concurrent `iris:cherry_pick` calls target the same source repo
- **THEN** the second blocks until the first releases the lock; the checkout-and-pick pair is atomic from other callers' view

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris cherry-pick <task-id> <commit> <target-branch>`
- **THEN** the same `verbs.CherryPick` Go function executes and prints the structured result
