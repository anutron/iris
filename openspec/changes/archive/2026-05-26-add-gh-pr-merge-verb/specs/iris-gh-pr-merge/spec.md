## ADDED Requirements

### Requirement: `iris:gh_pr_merge` verb

The plugin SHALL expose `iris:gh_pr_merge` as an MCP tool accepting `task_id` (string, required), `pr_number` (integer, required, minimum 1), and `strategy` (string enum {squash, merge, rebase}, default `squash`). On success the verb SHALL return `{merged: true, strategy}`. On failure the verb SHALL return a structured error wrapping gh's combined output and SHALL NOT invoke gh when the input fails validation.

#### Scenario: Successful squash merge

- **WHEN** the verb is invoked with `pr_number=7` and `strategy="squash"` for a known task
- **THEN** iris runs `gh pr merge 7 --squash` in the source repo and returns `{merged: true, strategy: "squash"}`

#### Scenario: Rebase strategy is passed through

- **WHEN** the verb is invoked with `strategy="rebase"`
- **THEN** the gh invocation includes `--rebase` and the returned `strategy` echoes `"rebase"`

#### Scenario: Merge strategy is passed through

- **WHEN** the verb is invoked with `strategy="merge"`
- **THEN** the gh invocation includes `--merge` and the returned `strategy` echoes `"merge"`

#### Scenario: gh error surfaces stderr in the verb error

- **WHEN** gh exits non-zero with stderr (e.g., `required checks have failed`)
- **THEN** the verb returns an error whose message contains the stderr text so the caller can diagnose without rerunning gh

#### Scenario: Invalid strategy is rejected before gh is invoked

- **WHEN** the verb is invoked with `strategy="rebase-squash"` (or any value outside the enum)
- **THEN** the verb returns an error mentioning "strategy" and does NOT invoke gh

#### Scenario: Non-positive PR number is rejected before gh is invoked

- **WHEN** the verb is invoked with `pr_number=0` (or negative)
- **THEN** the verb returns an error mentioning "pr_number" and does NOT invoke gh

#### Scenario: Refuses an unknown task ID

- **WHEN** the verb is invoked with a `task_id` that argus does not recognize
- **THEN** iris returns a structured error naming the task ID and performs no gh invocation

#### Scenario: Refuses a source repo outside the project allowlist

- **WHEN** the resolved source-repo path does not match any allowlisted argus project
- **THEN** iris returns a structured error naming the rejected path and performs no gh invocation

#### Scenario: Direct CLI invocation runs the same verb

- **WHEN** the user runs `iris gh-pr-merge <task-id> --pr <N> [--strategy squash|merge|rebase]` from any shell on the host
- **THEN** the same `verbs.GHPRMerge` Go function executes (bypassing the daemon process) and prints the structured result
