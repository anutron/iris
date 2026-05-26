## Why

`iris:gh_pr_create` opens a PR; the matching `iris:gh_pr_merge` closes the loop by merging it. Agents inside argus worktrees cannot authenticate to GitHub directly, and even if they could, merging is a high-trust operation that ought to use the host's gh credentials. This verb is the host-side wrapper so an orchestrator agent can merge a PR by task ID + PR number without leaving the sandbox.

## What Changes

- Add `iris:gh_pr_merge` verb. Resolves source repo from `task_id`, validates `strategy` against the squash|merge|rebase enum, shells out to `gh pr merge <pr_number> --<strategy>` in the source repo.
- Wire MCP handler `iris_gh_pr_merge` and CLI subcommand `iris gh-pr-merge <task-id> --pr <N> [--strategy squash|merge|rebase]`.

## Capabilities

### New Capabilities

- `iris-gh-pr-merge`: The `iris:gh_pr_merge` verb specifically — input contract, strategy enum, gh subprocess invocation, structured response.

### Modified Capabilities

None.

## Impact

- Adds the merge half of the PR-lifecycle pair (with `iris:gh_pr_create`). Same gh CLI dependency, same host-credential reuse model.
- v1 has NO automatic CI status pre-check. gh's own merge returns non-zero if required checks are red, so the failure mode is surfaced from gh's stderr. Adding an explicit `gh pr checks` pre-call is non-breaking and tracked in design.md Open Questions.
- Narrow surface: no reviewer dismissal, no auto-merge enablement, no body templating. The verb merges what already exists.
