## ADDED Requirements

### Requirement: `iris:gh_pr_create` verb

The plugin SHALL expose `iris:gh_pr_create` as an MCP tool accepting `task_id` (string, required), `title` (string, required), `body` (string, optional), and `draft` (bool, default false). On success the verb SHALL return `{number, url}` where `number` is parsed from the last non-empty line of `gh pr create`'s stdout matching `/pull/<N>`. On failure the verb SHALL return a structured error wrapping gh's combined output.

#### Scenario: Successful PR creation

- **WHEN** the verb is invoked for a task whose branch is `argus/<slug>` and has been pushed to origin
- **THEN** iris runs `gh pr create --base <default> --head argus/<slug> --title <T>` in the source repo, parses the PR URL from the last non-empty stdout line, and returns `{number, url}`

#### Scenario: Refuses to open a PR from the default branch

- **WHEN** the resolved task branch equals the source repo's default branch (`main` or `master`)
- **THEN** iris returns a structured error containing the phrase "default branch", does NOT invoke gh, and the source repo and origin are unchanged

#### Scenario: Draft flag is passed through

- **WHEN** the verb is invoked with `draft=true`
- **THEN** the gh invocation includes the `--draft` argument; with `draft=false` (or omitted) `--draft` is absent

#### Scenario: Empty body omits the flag

- **WHEN** the verb is invoked with an empty `body`
- **THEN** the gh invocation does NOT include `--body`, allowing gh's pull-request-template default to apply

#### Scenario: gh auth failure surfaces actionable error

- **WHEN** gh exits non-zero with stderr containing `gh auth login`
- **THEN** the verb returns an error whose message includes `gh auth login` so the operator knows what to run

#### Scenario: Refuses an unknown task ID

- **WHEN** the verb is invoked with a `task_id` that argus does not recognize
- **THEN** iris returns a structured error naming the task ID and performs no gh invocation

#### Scenario: Title is required

- **WHEN** the verb is invoked with an empty `title`
- **THEN** the verb returns an error before invoking gh; the handler's input validation also catches this independently

#### Scenario: Direct CLI invocation runs the same verb

- **WHEN** the user runs `iris gh-pr-create <task-id> --title T [--body B] [--draft]` from any shell on the host
- **THEN** the same `verbs.GHPRCreate` Go function executes (bypassing the daemon process) and prints the structured result
