# Design: PR management verbs

## Context

v1.0 ships two gh-shelling verbs: `iris:gh_pr_create` (open a PR) and `iris:gh_pr_merge` (merge a PR). They share the same shape: typed input, source-repo lock, shell out to gh, parse stdout, return typed result. Four more gh operations fit cleanly within that shape and are realistic agent needs.

This change is mostly mechanical: four new verbs in the same shape as the v1.0 gh-shelling verbs.

## Goals

- Round out gh PR coverage to what an agent realistically needs in a decision loop (read state, take a draft live, comment, close).
- Reuse the v1.0 patterns verbatim: fake-gh PATH override for tests, structured JSON results, per-source-repo lock for the duration of the shellout.

## Non-goals

- No PR creation variants (label management, milestone, assignees). gh exposes these but the agent rarely needs them; if a real workflow surfaces, add them then.
- No GraphQL-backed verbs. `gh` CLI is the contract; iris does not wrap go-github.

## Decisions

### D1. One gh call per verb; no compound

Each verb shells out to one gh subcommand and returns one structured result. No compound verbs (e.g., "view then conditionally comment"). Composition stays in the agent, which can chain calls.

### D2. JSON output preference

Where gh supports `--json <fields>` (view), iris uses it and returns parsed JSON. Where gh emits text (ready, comment, close), iris parses the text per gh's documented output shape and returns a small typed result.

### D3. Shared error path

All four verbs return the existing `*GHCLIError`-style structured error on non-zero gh exit, carrying stdout and stderr. Matches v1.0 verbs.

### D4. Lock scope is the gh shellout

Per-source-repo lock is held from immediately after `verbs.Resolve` returns through gh's exit. gh CLI does not mutate the source repo's working tree (unless `gh pr close --delete-branch` deletes a local branch, which happens after merge in v1.0's complete_task flow), so the lock is conservative-but-cheap.

### D5. Optional flags surface as typed inputs

- `iris:gh_pr_comment`: required `body` (string). gh's `--body-file` is not surfaced (agent can pass body inline).
- `iris:gh_pr_close`: optional `delete_branch` (bool, default false).
- `iris:gh_pr_ready`: no flags.
- `iris:gh_pr_view`: no flags (iris hardcodes the JSON field list).

## Acceptance criteria

### `iris:gh_pr_view`

- it should run `gh pr view <pr_number> --json state,checks,reviews,mergeable,headRefName,baseRefName,isDraft,statusCheckRollup` in the resolved source repo and return the parsed JSON unchanged
- it should refuse with a structured error when gh exits non-zero, carrying gh's stdout and stderr
- it should refuse when the resolved source repo is not on the argus project allowlist
- it should hold the per-source-repo lock for the duration of the gh shellout
- it should mirror the same Go function from CLI invocation (`iris gh-pr-view <task-id> --pr <n>`)

### `iris:gh_pr_ready`

- it should run `gh pr ready <pr_number>` in the resolved source repo and return `{ ready: true, was_draft: <bool> }`
- it should refuse on non-zero gh exit with a structured error containing gh's output
- it should be idempotent: if the PR is already ready, gh exits 0 and iris returns `was_draft: false`
- it should mirror the same Go function from CLI invocation

### `iris:gh_pr_comment`

- it should run `gh pr comment <pr_number> --body <body>` in the resolved source repo and return `{ url: <comment_url> }` parsed from gh's stdout
- it should refuse when `body` is empty (no zero-length comments)
- it should refuse on non-zero gh exit with a structured error containing gh's output
- it should mirror the same Go function from CLI invocation

### `iris:gh_pr_close`

- it should run `gh pr close <pr_number>` (with `--delete-branch` appended when `delete_branch = true`) in the resolved source repo and return `{ closed: true, branch_deleted: <bool> }`
- it should refuse on non-zero gh exit with a structured error containing gh's output
- it should NOT consult the argus project allowlist beyond what `verbs.Resolve` already does
- it should mirror the same Go function from CLI invocation

## Risks / Trade-offs

- **Risk:** gh's stdout format changes across versions. → Mitigation: where iris parses gh's stdout (comment URL, close confirmation), parsing is best-effort; on parse failure, iris returns the raw stdout in the result and notes a `parse_warning`.
- **Trade-off:** Four near-identical verb files is repetitive. Acceptable — matches v1.0 pattern, keeps each verb file simple and testable in isolation.

## Migration plan

1. Ship this change on the same feature branch as `add-daemon-self-management` and `add-source-repo-utility-verbs`.
2. Dogfood by using `iris:gh_pr_view` in the dogfood loop for `add-daemon-self-management` (verify the archive PR is mergeable before calling `iris:gh_pr_merge`).
3. After dogfood, archive.
4. Rollback: revert. No state to clean up.

## Open questions

None.
