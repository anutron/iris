# iris-gh-pr-view Specification

## ADDED Requirements

### Requirement: `iris:gh_pr_view` verb

The plugin SHALL expose `iris:gh_pr_view` as an MCP tool and CLI subcommand that reads a GitHub PR's state via the host `gh` CLI from the canonical source repo. The verb SHALL accept `task_id` (required, string) and `pr_number` (required, integer ≥ 1). On success, the verb SHALL return the parsed JSON object from `gh pr view --json state,checks,reviews,mergeable,headRefName,baseRefName,isDraft,statusCheckRollup`.

#### Scenario: Successful view returns parsed JSON

- **WHEN** `iris:gh_pr_view` is invoked with `task_id` and `pr_number = N`
- **THEN** iris runs `gh pr view N --json state,checks,reviews,mergeable,headRefName,baseRefName,isDraft,statusCheckRollup` in the resolved source repo and returns the parsed JSON unchanged

#### Scenario: Non-zero gh exit returns structured error

- **WHEN** gh exits non-zero (e.g., the PR does not exist, or the repo is not a gh remote)
- **THEN** iris returns a structured error carrying gh's stdout and stderr

#### Scenario: Refuses unknown task ID

- **WHEN** the verb is invoked with a `task_id` argus does not recognize
- **THEN** iris returns a structured error and does NOT shell out to gh

#### Scenario: Refuses non-allowlisted source repo

- **WHEN** the resolved source repo is not on the argus project allowlist
- **THEN** iris returns a structured error naming the rejected path

#### Scenario: Per-source-repo lock held for gh shellout

- **WHEN** two concurrent `iris:gh_pr_view` calls target the same source repo
- **THEN** the second call blocks until the first releases the lock

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris gh-pr-view <task-id> --pr N` from any shell
- **THEN** the same `verbs.GhPrView` Go function executes and prints the structured result as pretty-printed JSON
