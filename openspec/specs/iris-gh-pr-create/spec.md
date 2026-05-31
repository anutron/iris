# iris-gh-pr-create Specification

## Purpose
TBD - created by archiving change add-gh-pr-create-verb. Update Purpose after archive.
## Requirements
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

#### Scenario: Refuses a source repo outside the project allowlist

- **WHEN** the resolved source-repo path does not match any allowlisted argus project
- **THEN** iris returns a structured error naming the rejected path and performs no gh invocation

#### Scenario: Title is required

- **WHEN** the verb is invoked with an empty `title`
- **THEN** the verb returns an error before invoking gh; the handler's input validation also catches this independently

#### Scenario: Direct CLI invocation runs the same verb

- **WHEN** the user runs `iris gh-pr-create <task-id> --title T [--body B] [--draft]` from any shell on the host
- **THEN** the same `verbs.GHPRCreate` Go function executes (bypassing the daemon process) and prints the structured result

### Requirement: Cross-fork pull requests

`iris:gh_pr_create` SHALL support opening a pull request from a fork into its upstream parent. When the resolved source repo's `origin` is a GitHub fork (its repository has a parent), `iris:gh_pr_create` SHALL target the upstream parent: it SHALL invoke `gh pr create --repo <upstream-owner>/<upstream-repo> --head <fork-owner>:<branch>` (with `--title`, and `--body`/`--draft` when supplied), letting gh default the base to the upstream repository's default branch. When `origin` is NOT a fork, `iris:gh_pr_create` SHALL behave as before, opening a same-repo PR with explicit `--base <default-branch> --head <branch>`.

Fork detection SHALL be best-effort and SHALL NOT regress the common case: if iris cannot determine the fork relationship (the detection command errors, or returns no parent), it SHALL fall back to the same-repo behavior. The existing refusal to open a PR from the source repo's default branch SHALL still apply in both cases.

#### Scenario: Fork origin opens a cross-fork PR against upstream

- **GIVEN** a resolved source repo whose `origin` is a fork `<fork-owner>/<repo>` of upstream `<upstream-owner>/<repo>`, on a non-default branch
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris runs `gh pr create --repo <upstream-owner>/<repo> --head <fork-owner>:<branch> --title <title>` (plus `--body`/`--draft` when supplied) and returns the resulting PR number and URL

#### Scenario: Non-fork origin opens a same-repo PR

- **GIVEN** a resolved source repo whose `origin` is not a fork (its repository has no parent)
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris runs `gh pr create --base <default-branch> --head <branch> --title <title>` as before — it does NOT add `--repo` or a fork-qualified head

#### Scenario: Detection failure falls back to same-repo

- **GIVEN** a resolved source repo where the fork-detection command fails (e.g. gh cannot reach GitHub)
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris does NOT abort on the detection failure; it falls back to the same-repo `--base/--head` invocation

#### Scenario: Default-branch refusal still applies

- **GIVEN** a resolved source repo whose current branch is the default branch
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris refuses with the default-branch error and does NOT invoke gh, regardless of whether `origin` is a fork

