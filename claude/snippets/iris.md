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
