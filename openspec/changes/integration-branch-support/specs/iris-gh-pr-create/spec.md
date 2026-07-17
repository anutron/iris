## MODIFIED Requirements

### Requirement: `iris:gh_pr_create` verb

The plugin SHALL expose `iris:gh_pr_create` as an MCP tool accepting `task_id` (string, required), `title` (string, required), `body` (string, optional), `draft` (bool, default false), `head` (string, optional), `base_repo` (string, optional), and `base` (string, optional). When `head` is provided and non-empty, the verb SHALL open the PR for that head branch; when `head` is omitted or empty, the verb SHALL open the PR for the task's resolved branch. The branch the PR is opened for is the "effective head". `base`, when provided and non-empty, SHALL select the target **branch** within whichever repository is selected by `base_repo` / fork-detection / origin (an independent axis from `base_repo`, which selects the target **repository**); when `base` is omitted or empty, behavior is unchanged (gh defaults the base to the target repository's own default branch in `base_repo`/fork modes, or iris passes the resolved source-repo default branch explicitly in same-repo-on-origin mode). On success the verb SHALL return `{number, url}` where `number` is parsed from the last non-empty line of `gh pr create`'s stdout matching `/pull/<N>`. On failure the verb SHALL return a structured error wrapping gh's combined output. The default-branch refusal and source-repo allowlist SHALL apply to the effective head. The verb SHALL reject an effective head that begins with `-` before invoking gh, so a head override cannot smuggle flags into `gh pr create` (nor poison the fork-qualified `owner:branch` join). When `base_repo` is provided it governs the target repository as defined in the "Cross-fork pull requests" requirement; the verb SHALL reject a `base_repo` that begins with `-` or is not `owner/repo` shaped before invoking gh. The verb SHALL reject a `base` that begins with `-` before invoking gh.

#### Scenario: Successful PR creation

- **WHEN** the verb is invoked without `head`, `base_repo`, or `base` for a task whose branch is `argus/<slug>` and has been pushed to origin
- **THEN** iris runs `gh pr create --base <default> --head argus/<slug> --title <T>` in the source repo, parses the PR URL from the last non-empty stdout line, and returns `{number, url}`

#### Scenario: Explicit head override opens the PR for the named branch

- **WHEN** the verb is invoked with `head="feature-x"` for a task whose resolved branch is `argus/<slug>`, and `feature-x` is pushed to origin
- **THEN** iris runs `gh pr create --base <default> --head feature-x --title <T>` (NOT the resolved task branch) and returns `{number, url}`

#### Scenario: Rejects a head override beginning with a dash

- **WHEN** the verb is invoked with a `head` override that begins with `-` (e.g. `--upload-pack=evil`)
- **THEN** iris returns a structured error stating the head must not begin with `-`, does NOT invoke gh, and the source repo and origin are unchanged

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

#### Scenario: Explicit base overrides the target branch in same-repo mode

- **GIVEN** no `base_repo` and origin is not a fork
- **WHEN** the verb is invoked with `base="integration/big-feature"`
- **THEN** iris runs `gh pr create --base integration/big-feature --head <effective-head> --title <T>` instead of the resolved default branch

#### Scenario: Explicit base composes with base_repo

- **GIVEN** the verb is invoked with `base_repo="drn/argus"` and `base="release/1.2"`
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris runs `gh pr create --repo drn/argus --base release/1.2 --head <effective-head> --title <T>` — `base` is appended even though `base_repo` mode would otherwise omit `--base`

#### Scenario: Explicit base composes with cross-fork auto-detection

- **GIVEN** a resolved source repo whose origin is a detected fork, no `base_repo`, and the verb is invoked with `base="release/1.2"`
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris runs `gh pr create --repo <upstream-owner>/<repo> --base release/1.2 --head <fork-owner>:<effective-head> --title <T>` — `base` is appended even though cross-fork mode would otherwise omit `--base`

#### Scenario: Omitted base preserves existing per-mode default-branch behavior

- **WHEN** the verb is invoked with `base` omitted or empty, in any of the three target modes (`base_repo`, cross-fork, same-repo-on-origin)
- **THEN** the gh invocation is unchanged from before `base` existed: `--base` omitted in `base_repo`/cross-fork mode, `--base <default-branch>` in same-repo-on-origin mode

#### Scenario: Rejects a base beginning with a dash

- **WHEN** the verb is invoked with a `base` that begins with `-` (e.g. `--upload-pack=evil`)
- **THEN** iris returns a structured error stating `base` must not begin with `-`, does NOT invoke gh, and the source repo and origin are unchanged

#### Scenario: Direct CLI invocation runs the same verb

- **WHEN** the user runs `iris gh-pr-create <task-id> --title T [--body B] [--draft] [--head <name>] [--base-repo <owner/repo>] [--base <branch>]` from any shell on the host
- **THEN** the same `verbs.GHPRCreate` Go function executes (bypassing the daemon process) and prints the structured result

### Requirement: Cross-fork pull requests

`iris:gh_pr_create` SHALL support opening a pull request from a fork into its upstream parent, and SHALL support an explicit `base_repo` override that opens a same-repo PR on a named repository. Target selection SHALL follow this precedence: (1) when `base_repo` is provided and non-empty, the verb SHALL open a **same-repo PR on `base_repo`**, invoking `gh pr create --repo <base_repo> --head <effective-head>` (with `--title`, `--body`/`--draft` when supplied, and `--base <base>` when `base` is non-empty — otherwise omitting `--base` so gh defaults to `base_repo`'s default branch), and SHALL NOT fork-qualify the head and SHALL NOT consult fork detection; (2) otherwise, when the resolved source repo's `origin` is a GitHub fork (its repository has a parent), the verb SHALL target the upstream parent, invoking `gh pr create --repo <upstream-owner>/<upstream-repo> --head <fork-owner>:<effective-head>` (plus `--base <base>` when `base` is non-empty, otherwise omitted so gh defaults the base to the upstream repository's default branch); (3) otherwise the verb SHALL open a same-repo PR on origin with explicit `--base <base if non-empty, else default-branch> --head <effective-head>`. In all cases the effective head is the `head` override when provided and non-empty, else the task's resolved branch, and `base`, when non-empty, is validated as not beginning with `-` before gh runs.

Fork detection SHALL be best-effort and SHALL NOT regress the common case: if iris cannot determine the fork relationship (the detection command errors, or returns no parent), it SHALL fall back to the same-repo-on-origin behavior. The existing refusal to open a PR from the source repo's default branch SHALL still apply in all cases, evaluated against the effective head.

#### Scenario: Explicit base_repo opens a same-repo PR on the named repository

- **GIVEN** the verb is invoked with `base_repo="drn/argus"` and a non-default effective head `feature-x`
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris runs `gh pr create --repo drn/argus --head feature-x --title <title>` (plus `--body`/`--draft` when supplied), omits `--base`, does NOT fork-qualify the head, and returns the resulting PR number and URL

#### Scenario: base_repo takes precedence over fork auto-detection

- **GIVEN** a resolved source repo whose `origin` is a fork `<fork-owner>/<repo>` of an upstream, and the verb is invoked with `base_repo="<upstream-owner>/<repo>"`
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris produces the same-repo-on-`base_repo` invocation (`--repo <upstream-owner>/<repo> --head <effective-head>`) and does NOT fork-qualify the head — `base_repo` wins over auto-detection

#### Scenario: Rejects a malformed base_repo

- **WHEN** the verb is invoked with a `base_repo` that begins with `-` or is not `owner/repo` shaped
- **THEN** iris returns a structured error, does NOT invoke gh, and the source repo and origin are unchanged

#### Scenario: Fork origin opens a cross-fork PR against upstream

- **GIVEN** a resolved source repo whose `origin` is a fork `<fork-owner>/<repo>` of upstream `<upstream-owner>/<repo>`, with an effective head that is a non-default branch, and no `base_repo`
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris runs `gh pr create --repo <upstream-owner>/<repo> --head <fork-owner>:<effective-head> --title <title>` (plus `--body`/`--draft` when supplied) and returns the resulting PR number and URL

#### Scenario: Fork origin fork-qualifies an explicit head override

- **GIVEN** a resolved source repo whose `origin` is a fork `<fork-owner>/<repo>`, no `base_repo`, and the verb is invoked with `head="feature-x"`
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris runs `gh pr create --repo <upstream-owner>/<repo> --head <fork-owner>:feature-x --title <title>` — the override is fork-qualified, not the resolved task branch

#### Scenario: Non-fork origin opens a same-repo PR

- **GIVEN** a resolved source repo whose `origin` is not a fork (its repository has no parent), and no `base_repo`
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris runs `gh pr create --base <default-branch> --head <effective-head> --title <title>` as before — it does NOT add `--repo` or a fork-qualified head

#### Scenario: Detection failure falls back to same-repo

- **GIVEN** a resolved source repo where the fork-detection command fails (e.g. gh cannot reach GitHub), and no `base_repo`
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris does NOT abort on the detection failure; it falls back to the same-repo `--base/--head` invocation

#### Scenario: Default-branch refusal still applies

- **GIVEN** a resolved source repo whose effective head is the default branch
- **WHEN** `iris:gh_pr_create` is invoked
- **THEN** iris refuses with the default-branch error and does NOT invoke gh, regardless of whether `origin` is a fork or `base_repo` is set
