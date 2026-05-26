# Add PR management verbs

## Why

v1.0 lets iris open and merge GitHub PRs (`iris:gh_pr_create`, `iris:gh_pr_merge`). It does not let iris read PR state, take a draft out of draft, comment, or close without merging. Each of these is a host-side gh CLI operation an agent inside a sandbox cannot reach directly (gh auth lives on the host). Agent workflows that need any PR state in their decision loop are blocked.

This change adds four read-and-write PR verbs that round out the gh-shelling surface to what an agent realistically needs.

## What Changes

- **New `iris:gh_pr_view <pr_number>` verb.** Shells out to `gh pr view --json state,checks,reviews,mergeable,headRefName,baseRefName,isDraft,statusCheckRollup`. Returns the parsed object. For "is this PR ready to merge" agent loops.
- **New `iris:gh_pr_ready <pr_number>` verb.** Shells out to `gh pr ready`. Takes a draft PR out of draft. Returns the resulting state.
- **New `iris:gh_pr_comment <pr_number> --body` verb.** Shells out to `gh pr comment`. Posts a comment. Returns the comment URL.
- **New `iris:gh_pr_close <pr_number>` verb.** Shells out to `gh pr close`. Closes the PR without merging. Optional `delete_branch` flag.

All four follow the existing v1.0 gh-shelling pattern: `task_id` resolves the source repo; gh runs from inside that repo; output is captured and parsed into a typed result; the per-source-repo lock is held for the duration of the shellout.

## Capabilities

### New Capabilities

- `iris-gh-pr-view`: `iris:gh_pr_view` verb — read PR state from gh as structured JSON.
- `iris-gh-pr-ready`: `iris:gh_pr_ready` verb — mark a draft PR as ready for review.
- `iris-gh-pr-comment`: `iris:gh_pr_comment` verb — post a comment to a PR.
- `iris-gh-pr-close`: `iris:gh_pr_close` verb — close a PR without merging.

### Modified Capabilities

None.

## Impact

- New Go files: `internal/verbs/gh_pr_view.go`, `gh_pr_ready.go`, `gh_pr_comment.go`, `gh_pr_close.go`, matching `internal/mcp/handler_*.go`, and matching `cmd/iris/*.go` Cobra subcommands.
- Registration in `internal/daemon/run.go`: 4 new tool definitions.
- No new dependencies. Reuses `os/exec` and the existing `runGh` helper from `internal/verbs/gh.go`.
- No changes to `iris-host-bridge`. All four verbs require `task_id` (not self-hosting).
