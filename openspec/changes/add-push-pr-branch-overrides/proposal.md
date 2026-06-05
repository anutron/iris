## Why

`iris:push` and `iris:gh_pr_create` resolve the branch solely from `task_id` (`resolved.Branch`), so a single argus task can only push and open PRs for its own branch. A session that legitimately juggles several branches (e.g. a QA task pushing fixes across four open PRs plus a new branch) has no way to drive push/PR-create through iris and falls back to host-side hands. `iris:gh_pr_merge` and `iris:gh_pr_view` already accept `pr_number` and are not branch-locked, so only push and PR-create carry this limitation.

## What Changes

- Add an optional `branch` parameter to `iris:push`. When provided, iris pushes that branch instead of the task's resolved branch; when omitted, behavior is unchanged.
- Add an optional `head` parameter to `iris:gh_pr_create`. When provided, iris opens the PR for that head branch instead of the task's resolved branch; when omitted, behavior is unchanged.
- The override is resolved within the repo that `task_id` already identifies — the new arg changes *which branch*, never *which repo*.
- Existing safety rails apply to the override unchanged: the default-branch refusal and the source-repo allowlist still gate the explicit branch.
- Cross-fork PR behavior composes automatically: an explicit `head` is fork-qualified the same way the resolved branch is today (`<fork-owner>:<head>`).
- Not breaking: both parameters are optional and default to the task's resolved branch.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `iris-push`: the verb accepts an optional `branch` override; the pushed branch is the override when present, else the task's resolved branch.
- `iris-gh-pr-create`: the verb accepts an optional `head` override; the PR head is the override when present, else the task's resolved branch.

## Impact

- `internal/verbs/push.go` — add `Branch` to `PushOptions`, select effective branch.
- `internal/verbs/gh_pr_create.go` — add `Head` to `GHPRCreateOptions`, select effective branch for both same-repo and fork-qualified head.
- `internal/mcp/handler_push.go` — accept and pass `branch`.
- `internal/mcp/handler_gh_pr_create.go` — accept and pass `head`.
- `cmd/iris/*.go` — add `--branch` / `--head` CLI flags to the matching subcommands.
- MCP tool schemas for `iris:push` and `iris:gh_pr_create`.
- No data, migration, or cross-service impact.
