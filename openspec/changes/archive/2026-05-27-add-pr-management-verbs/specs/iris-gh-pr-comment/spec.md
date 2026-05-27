# iris-gh-pr-comment Specification

## ADDED Requirements

### Requirement: `iris:gh_pr_comment` verb

The plugin SHALL expose `iris:gh_pr_comment` as an MCP tool and CLI subcommand that posts a comment to a GitHub PR via the host `gh` CLI. The verb SHALL accept `task_id` (required, string), `pr_number` (required, integer ≥ 1), and `body` (required, non-empty string). On success, the verb SHALL return `{ url: <comment_url> }` parsed from gh's stdout.

#### Scenario: Successful comment returns the comment URL

- **WHEN** `iris:gh_pr_comment` is invoked with a valid `body`
- **THEN** iris runs `gh pr comment <pr_number> --body <body>` in the resolved source repo and returns `{ url: <parsed_comment_url> }`

#### Scenario: Empty body is refused at input validation

- **WHEN** `body` is an empty string or missing
- **THEN** iris returns a structured error before shelling out to gh

#### Scenario: Non-zero gh exit returns structured error

- **WHEN** gh exits non-zero
- **THEN** iris returns a structured error carrying gh's stdout and stderr

#### Scenario: Comment URL parse failure surfaces a warning

- **WHEN** gh exits 0 but the stdout does not contain a parseable comment URL
- **THEN** iris returns `{ url: "" }` plus a `parse_warning` field containing the raw stdout

#### Scenario: Refuses unknown task ID

- **WHEN** invoked with an unknown `task_id`
- **THEN** iris returns a structured error and does NOT shell out to gh

#### Scenario: Refuses non-allowlisted source repo

- **WHEN** the resolved source repo is not on the argus project allowlist
- **THEN** iris returns a structured error naming the rejected path

#### Scenario: Per-source-repo lock held for gh shellout

- **WHEN** two concurrent `iris:gh_pr_comment` calls target the same source repo
- **THEN** the second blocks until the first releases the lock

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris gh-pr-comment <task-id> --pr N --body "..."`
- **THEN** the same `verbs.GhPrComment` Go function executes and prints the structured result
