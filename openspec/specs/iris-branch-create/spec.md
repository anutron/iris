# iris-branch-create Specification

## Purpose
TBD - created by archiving change add-branch-cherry-pick-checkout-verbs. Update Purpose after archive.
## Requirements
### Requirement: `iris:branch_create` verb

The plugin SHALL expose `iris:branch_create` as an MCP tool and CLI subcommand that creates a branch in the resolved source repo from an arbitrary ref. The verb SHALL accept `task_id` (required, string), `name` (required, non-empty string), and `base_ref` (required, non-empty string). On success, the verb SHALL return `{ created: true, branch, base_ref, sha }` and SHALL NOT change the source repo's current checkout.

#### Scenario: Successful branch creation returns SHA

- **WHEN** `iris:branch_create` is invoked with a valid `name` and a `base_ref` that resolves
- **THEN** iris runs `git branch <name> <base_ref>` in the resolved source repo and returns `{ created: true, branch: <name>, base_ref: <base_ref>, sha: <sha> }`

#### Scenario: Does not change current checkout

- **WHEN** `iris:branch_create` is invoked and succeeds
- **THEN** the source repo's HEAD points to the same ref it pointed to before the call

#### Scenario: Refuses empty name

- **WHEN** invoked with an empty `name`
- **THEN** iris returns a structured error naming the field and does NOT shell out to git

#### Scenario: Refuses empty base_ref

- **WHEN** invoked with an empty `base_ref`
- **THEN** iris returns a structured error naming the field and does NOT shell out to git

#### Scenario: Refuses leading-dash name (flag-smuggling guard)

- **WHEN** invoked with `name` that starts with `-`
- **THEN** iris returns an `invalid branch name` error and does NOT shell out to git

#### Scenario: Refuses leading-dash base_ref (flag-smuggling guard)

- **WHEN** invoked with `base_ref` that starts with `-`
- **THEN** iris returns an `invalid base_ref` error and does NOT shell out to git

#### Scenario: Refuses default branch name

- **WHEN** `name` equals the resolved default branch (or `main` or `master`)
- **THEN** iris returns a structured error naming both the requested name and the default branch

#### Scenario: Refuses invalid git ref name

- **WHEN** `name` is a string git itself would reject (e.g., contains `..`, ends in `.lock`, etc., as detected by `git check-ref-format --branch`)
- **THEN** iris returns a structured error naming the invalid name

#### Scenario: Refuses existing branch

- **WHEN** the local branch already exists
- **THEN** iris returns a structured error naming the conflict and does NOT mutate the repo

#### Scenario: Refuses unresolvable base_ref

- **WHEN** `base_ref` does not resolve (no such SHA, tag, or branch)
- **THEN** iris returns a structured error carrying git's stderr

#### Scenario: Refuses unknown task ID

- **WHEN** invoked with an unknown `task_id`
- **THEN** iris returns a structured error and does NOT shell out to git

#### Scenario: Refuses non-allowlisted source repo

- **WHEN** the resolved source repo is not on the argus project allowlist
- **THEN** iris returns a structured error naming the rejected path

#### Scenario: Per-source-repo lock held for branch creation

- **WHEN** two concurrent `iris:branch_create` calls target the same source repo
- **THEN** the second blocks until the first releases the lock

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris branch-create <task-id> <name> <base-ref>`
- **THEN** the same `verbs.BranchCreate` Go function executes and prints the structured result

