# iris-gh-pr-close Specification

## ADDED Requirements

### Requirement: `iris:gh_pr_close` verb

The plugin SHALL expose `iris:gh_pr_close` as an MCP tool and CLI subcommand that closes a GitHub PR without merging via the host `gh` CLI. The verb SHALL accept `task_id` (required, string), `pr_number` (required, integer ≥ 1), and `delete_branch` (optional, boolean, default false). On success, the verb SHALL return `{ closed: true, branch_deleted: <bool> }`.

#### Scenario: Close without branch deletion

- **WHEN** `iris:gh_pr_close` is invoked with `delete_branch = false` (or omitted)
- **THEN** iris runs `gh pr close <pr_number>` in the resolved source repo and returns `{ closed: true, branch_deleted: false }`

#### Scenario: Close with branch deletion

- **WHEN** `iris:gh_pr_close` is invoked with `delete_branch = true`
- **THEN** iris runs `gh pr close <pr_number> --delete-branch` and returns `{ closed: true, branch_deleted: true }`

#### Scenario: Non-zero gh exit returns structured error

- **WHEN** gh exits non-zero (PR not found, already closed, permission denied)
- **THEN** iris returns a structured error carrying gh's stdout and stderr

#### Scenario: Already-closed PR is treated per gh's behavior

- **WHEN** the PR is already closed and gh's exit status is non-zero with a "PR already closed" message
- **THEN** iris returns the structured error verbatim; the caller decides whether to treat already-closed as success

#### Scenario: Refuses unknown task ID

- **WHEN** invoked with an unknown `task_id`
- **THEN** iris returns a structured error and does NOT shell out to gh

#### Scenario: Refuses non-allowlisted source repo

- **WHEN** the resolved source repo is not on the argus project allowlist
- **THEN** iris returns a structured error naming the rejected path

#### Scenario: Per-source-repo lock held for gh shellout

- **WHEN** two concurrent `iris:gh_pr_close` calls target the same source repo
- **THEN** the second blocks until the first releases the lock

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris gh-pr-close <task-id> --pr N [--delete-branch]`
- **THEN** the same `verbs.GhPrClose` Go function executes and prints the structured result
