## Why

Iris resolves the target repo from whatever `task_id` the caller passes, including a task from another argus project. That is useful (a coordinator can merge a worker's PRs in another repo), but no response says which repo was acted on. PR numbers repeat across repos, so a wrong `task_id` silently merges the wrong repo's `#11` and the response looks identical. This happened in the October 18–19 Sketch session, without harm but without any way to tell.

## What Changes

- Every MCP response from a verb that resolved a source repo carries a `repo` object (`name`: `owner/name` parsed from the origin remote, `path`: the canonical local source-repo path). JSON success responses gain a top-level `repo` field, and error responses gain a trailing `repo:` line. Implemented once in the MCP server, not per verb.
- `iris_gh_pr_merge` and `iris_gh_pr_close` look the PR up before acting and return its `pr_url` and `pr_title`.
- `iris_gh_pr_merge` and `iris_gh_pr_close` accept an optional `expected_repo` (`owner/name`). When it is given and does not match the PR's repo, iris refuses before merging or closing.
- The iris skill documents the cross-project pattern: a `task_id` from another project acts on that project's repo, so check `repo` and pass `expected_repo` on merge and close.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `iris-host-bridge`: MCP responses name the resolved repo.
- `iris-gh-pr-merge`: PR identity in the response and an optional `expected_repo` guard.
- `iris-gh-pr-close`: PR identity in the response and an optional `expected_repo` guard.

## Impact

- `internal/verbs/resolve.go`, `resolve_self.go`: record each resolution into a request-scoped recorder.
- `internal/mcp/server.go`: annotate every callback response with the recorded repo.
- `internal/verbs/gh_pr_merge.go`, `gh_pr_close.go` and their handlers and CLI flags: a pre-flight `gh pr view`, `expected_repo`.
- `internal/daemon/run.go` tool descriptions and `claude/skills/iris/SKILL.md`.
- The change is additive: existing response fields are unchanged and `expected_repo` is optional.
