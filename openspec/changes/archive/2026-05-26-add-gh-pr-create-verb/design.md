## Context

`iris:gh_pr_create` is the third iris verb. It rides on top of `iris:push`: an agent merges or just commits inside the worktree, calls `iris:push` to land the branch on origin, then calls `iris:gh_pr_create` to open the PR. The verb is a thin wrapper around the host's `gh` CLI – iris does not talk to the GitHub API directly because gh handles auth, host resolution (github.com vs Enterprise), and remote ↔ owner/repo inference far better than a hand-rolled client would.

## Goals / Non-Goals

**Goals:**

- One subprocess call to `gh pr create`, output parsed for the PR URL on the last non-empty line.
- Refuse to open a PR from the default branch (consistent with the safety stance in `iris:push`).
- Surface gh's stderr in the verb error so common failures (`gh auth login`, no upstream branch, no matching head ref on origin) are diagnosable from the verb error alone.

**Non-Goals:**

- Reviewer / label / milestone / assignee selection. Adding flags later is non-breaking. The verb stays narrow.
- Template substitution from commit messages. The caller (agent) crafts the body.
- Direct GitHub API integration. gh is the shim; iris stays out of GraphQL.
- Holding the per-source-repo mutex. `gh pr create` reads remote state and emits one POST; it does not mutate the local source repo's working tree. Letting two PR-create calls overlap is fine.

## Decisions

### gh runs in the source repo, not the worktree

The same reasoning as `iris:push`: `git worktree` makes the worktree's branch a ref in the source repo's refs/heads, so gh's source-repo invocation sees the branch via origin. Running in the worktree would also work but it would tie the verb to the worktree being clean.

### URL parsing is regex on the last non-empty stdout line

`gh pr create` is designed to be scriptable: on success the final line of stdout is `https://github.com/<owner>/<repo>/pull/<N>`. We pull the number with a small regex (`/pull/(\d+)`) rather than parsing the URL structurally — gh has been emitting this shape since 2020 and an enterprise host produces the same form. If gh ever stabilises on a JSON output mode for this command we switch then.

### Empty body omits the flag entirely

Passing `--body ""` to gh would emit an empty-bodied PR and skip gh's default-template inference. Omitting `--body` lets gh use the user's pull request template if one is defined in the repo. Caller can still force an empty body by passing a single space if desired.

### Verb-level title required check

The handler validates `title != ""` before calling the verb, and the verb validates again on the trimmed input. Defense in depth: the CLI marks the flag required so cobra catches it pre-call; the handler catches it from MCP input; the verb catches it from the CLI's RunE path. Three layers cost very little and stop a confusing gh error.

## Risks / Trade-offs

- **[Risk] gh stdout format drift.** gh prints the URL last on success today. If a future gh release adds trailing diagnostic lines (e.g., a "Creating PR for #1" note after the URL), our regex would miss. Mitigation: the regex scans for `/pull/<N>` against the line we picked, so a near-URL line still parses; if it fails, the verb returns the full output in the error.
- **[Trade-off] No CI gate on the PR body.** v1 caller is responsible for writing a useful body. The companion `/cortex:review` or human review handles content quality.
- **[Risk] gh subprocess uses host PATH.** A misconfigured PATH that lacks gh produces an obvious `exec: "gh"` error. setup.sh should warn if gh is missing; that's a follow-up.
- **[Trade-off] No mutex.** Two PR-create calls for the same task could in theory race and have gh complain "PR already exists" on the second. That's an agent-orchestration bug we surface, not a corruption risk.

## Migration Plan

- Verb-only addition. No installer changes, no host state, no schema migration.
- Rollback: revert the commit; the daemon stops registering `iris_gh_pr_create` and the tool disappears from argus's allowlist on next heartbeat.

## Open Questions

- Should setup.sh detect `gh` on PATH and `gh auth status` and bail with an actionable error? Reasonable follow-up; not blocking v1.
