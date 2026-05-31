# iris-gh-pr-create Specification

## ADDED Requirements

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
