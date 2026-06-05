# iris-gh-pr-create Specification

## MODIFIED Requirements

### Requirement: `iris:gh_pr_create` verb

The plugin SHALL expose `iris:gh_pr_create` as an MCP tool accepting `task_id` (string, required), `title` (string, required), `body` (string, optional), `draft` (bool, default false), and `head` (string, optional). When `head` is provided and non-empty, the verb SHALL open the PR for that head branch; when `head` is omitted or empty, the verb SHALL open the PR for the task's resolved branch. The branch the PR is opened for is the "effective head". On success the verb SHALL return `{number, url}` where `number` is parsed from the last non-empty line of `gh pr create`'s stdout matching `/pull/<N>`. On failure the verb SHALL return a structured error wrapping gh's combined output. The default-branch refusal and source-repo allowlist SHALL apply to the effective head.

#### Scenario: Successful PR creation

- **WHEN** the verb is invoked without `head` for a task whose branch is `argus/<slug>` and has been pushed to origin
- **THEN** iris runs `gh pr create --base <default> --head argus/<slug> --title <T>` in the source repo, parses the PR URL from the last non-empty stdout line, and returns `{number, url}`

#### Scenario: Explicit head override opens the PR for the named branch

- **WHEN** the verb is invoked with `head="feature-x"` for a task whose resolved branch is `argus/<slug>`, and `feature-x` is pushed to origin
- **THEN** iris runs `gh pr create --base <default> --head feature-x --title <T>` (NOT the resolved task branch) and returns `{number, url}`

#### Scenario: Refuses to open a PR from the default branch

- **WHEN** the effective head (resolved task branch, or the `head` override) equals the source repo's default branch (`main` or `master`)
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

#### Scenario: Refuses a source repo outside the project allowlist

- **WHEN** the resolved source-repo path does not match any allowlisted argus project
- **THEN** iris returns a structured error naming the rejected path and performs no gh invocation

#### Scenario: Title is required

- **WHEN** the verb is invoked with an empty `title`
- **THEN** the verb returns an error before invoking gh; the handler's input validation also catches this independently

#### Scenario: Direct CLI invocation runs the same verb

- **WHEN** the user runs `iris gh-pr-create <task-id> --title T [--body B] [--draft] [--head <name>]` from any shell on the host
- **THEN** the same `verbs.GHPRCreate` Go function executes (bypassing the daemon process) and prints the structured result

### Requirement: Cross-fork pull requests

`iris:gh_pr_create` SHALL support opening a pull request from a fork into its upstream parent. When the resolved source repo's `origin` is a GitHub fork (its repository has a parent), `iris:gh_pr_create` SHALL target the upstream parent: it SHALL invoke `gh pr create --repo <upstream-owner>/<upstream-repo> --head <fork-owner>:<effective-head>` (with `--title`, and `--body`/`--draft` when supplied), letting gh default the base to the upstream repository's default branch. When `origin` is NOT a fork, `iris:gh_pr_create` SHALL behave as before, opening a same-repo PR with explicit `--base <default-branch> --head <effective-head>`. In both cases the effective head is the `head` override when provided and non-empty, else the task's resolved branch.

Fork detection SHALL be best-effort and SHALL NOT regress the common case: if iris cannot determine the fork relationship (the detection command errors, or returns no parent), it SHALL fall back to the same-repo behavior. The existing refusal to open a PR from the source repo's default branch SHALL still apply in both cases, evaluated against the effective head.

#### Scenario: Fork origin opens a cross-fork PR against upstream

- **GIVEN** a resolved source repo whose `origin` is a fork `<fork-owner>/<repo>` of upstream `<upstream-owner>/<repo>`, with an effective head that is a non-default branch
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris runs `gh pr create --repo <upstream-owner>/<repo> --head <fork-owner>:<effective-head> --title <title>` (plus `--body`/`--draft` when supplied) and returns the resulting PR number and URL

#### Scenario: Fork origin fork-qualifies an explicit head override

- **GIVEN** a resolved source repo whose `origin` is a fork `<fork-owner>/<repo>`, and the verb is invoked with `head="feature-x"`
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris runs `gh pr create --repo <upstream-owner>/<repo> --head <fork-owner>:feature-x --title <title>` — the override is fork-qualified, not the resolved task branch

#### Scenario: Non-fork origin opens a same-repo PR

- **GIVEN** a resolved source repo whose `origin` is not a fork (its repository has no parent)
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris runs `gh pr create --base <default-branch> --head <effective-head> --title <title>` as before — it does NOT add `--repo` or a fork-qualified head

#### Scenario: Detection failure falls back to same-repo

- **GIVEN** a resolved source repo where the fork-detection command fails (e.g. gh cannot reach GitHub)
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris does NOT abort on the detection failure; it falls back to the same-repo `--base/--head` invocation

#### Scenario: Default-branch refusal still applies

- **GIVEN** a resolved source repo whose effective head is the default branch
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris refuses with the default-branch error and does NOT invoke gh, regardless of whether `origin` is a fork
