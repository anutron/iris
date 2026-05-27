# iris-gh-pr-ready Specification

## Purpose
TBD - created by archiving change add-pr-management-verbs. Update Purpose after archive.
## Requirements
### Requirement: `iris:gh_pr_ready` verb

The plugin SHALL expose `iris:gh_pr_ready` as an MCP tool and CLI subcommand that takes a draft GitHub PR out of draft via the host `gh` CLI. The verb SHALL accept `task_id` (required, string) and `pr_number` (required, integer ≥ 1). On success, the verb SHALL return `{ ready: true, was_draft: <bool> }` where `was_draft` reflects whether the PR was in draft state before the call.

#### Scenario: Draft PR is marked ready

- **WHEN** `iris:gh_pr_ready` is invoked for a PR currently in draft state
- **THEN** iris runs `gh pr ready <pr_number>` in the resolved source repo and returns `{ ready: true, was_draft: true }`

#### Scenario: Already-ready PR is idempotent

- **WHEN** `iris:gh_pr_ready` is invoked for a PR that is already ready for review
- **THEN** gh exits 0 (idempotent) and iris returns `{ ready: true, was_draft: false }`

#### Scenario: Non-zero gh exit returns structured error

- **WHEN** gh exits non-zero (PR not found, permission denied, etc.)
- **THEN** iris returns a structured error carrying gh's stdout and stderr

#### Scenario: Refuses unknown task ID

- **WHEN** invoked with an unknown `task_id`
- **THEN** iris returns a structured error and does NOT shell out to gh

#### Scenario: Refuses non-allowlisted source repo

- **WHEN** the resolved source repo is not on the argus project allowlist
- **THEN** iris returns a structured error naming the rejected path

#### Scenario: Per-source-repo lock held for gh shellout

- **WHEN** two concurrent `iris:gh_pr_ready` calls target the same source repo
- **THEN** the second blocks until the first releases the lock

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris gh-pr-ready <task-id> --pr N`
- **THEN** the same `verbs.GhPrReady` Go function executes and prints the structured result

