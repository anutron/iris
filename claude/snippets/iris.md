---
tags: [iris, argus]
audience: [shared]
---
If `ARGUS_TASK_ID` is unset and `$PWD` is not under `~/.argus/worktrees/`, ignore this section — iris's MCP tools are not registered outside an argus sandbox.

## iris — host-side git/gh from an argus sandbox

Inside an argus worktree, anything that touches the **host** — pushing, GitHub PRs, merging to the default branch, tagging — must go through iris's `mcp__argus__iris_*` tools, not `Bash`. Your worktree's local git (`status`, `diff`, `log`, `add`, `commit`, `branch`) is fine as plain `Bash`; only host-reaching ops need iris. Every iris verb resolves the source repo from `task_id`.

Most common redirects:

- `Bash(git push)` → `mcp__argus__iris_push(task_id)`
- `Bash(gh pr create|view|merge|comment)` → `mcp__argus__iris_gh_pr_create|view|merge|comment(task_id, …)`
- `Bash(git checkout master && git merge …)` → `mcp__argus__iris_merge_to_master(task_id)`
- ship-and-clean-up in one call → `mcp__argus__iris_complete_task(task_id)`

For the full tool map (branch/cherry-pick/tag/build/recovery verbs, decision rules, sibling-plugin composition, and worked workflows), load the `iris` skill.

Dogfood testing requires a lease: claim, deploy/test, then call `iris_dogfood_release` immediately when done or the user is away. Claims and renewals include a `dogfood_lease` snapshot; `iris_status` shows `holder.expires_at`, last Iris contact, and queue estimates. The default absolute hold limit is one hour; renewal cannot extend it. Valid in-flight deployments defer automatic release only within their bounded mark. Completed, archived or stopped tasks release on the next tick. Coordinators reserve the next merge window with `iris_dogfood_claim(task_id, position=1)` without SHA; if already queued with a commit, release that request first. Compose/deploy/test and release promptly.
