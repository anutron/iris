## Context

`iris:push` (`internal/verbs/push.go`) and `iris:gh_pr_create` (`internal/verbs/gh_pr_create.go`) both call `Resolve(ctx, client, taskID)` and operate strictly on `resolved.Branch` — the branch argus associates with the task. There is no way to act on any other branch in the same repo.

This encodes a one-task = one-branch = one-PR model. It breaks when a single task legitimately spans several branches: a QA/dogfood session pushing fixes across four already-open PRs, plus a new branch. In that case the verbs would push/PR the task's own branch every time, which is wrong, so the operator falls back to host-side git/gh by hand. The sibling verbs `iris:gh_pr_merge` and `iris:gh_pr_view` already accept `pr_number` and are not branch-locked — only push and PR-create are.

The repo identity is correct to keep on `task_id`: a task maps to one source repo via `Resolve`, and the allowlist gate depends on that resolved path. The only thing that needs to vary is the branch.

## Goals / Non-Goals

**Goals:**

- Let `iris:push` push a caller-named branch in the task's repo.
- Let `iris:gh_pr_create` open a PR for a caller-named head in the task's repo.
- Preserve today's behavior exactly when the override is omitted (backward-compatible).
- Keep the default-branch refusal and source-repo allowlist applied to the override.

**Non-Goals:**

- Changing which repo a task resolves to (still `task_id` → one source repo).
- Pre-validating that the branch exists or is pushed — git/gh own that error.
- Touching `iris:gh_pr_merge` / `iris:gh_pr_view` (already pr_number-based).
- A multi-branch or batch mode — this is a single optional override per call.

## Decisions

**1. Optional override parameter, defaulting to the resolved branch.**

Add `Branch string` to `PushOptions` and `Head string` to `GHPRCreateOptions`. In each verb, compute an effective branch: `effective := resolved.Branch; if override != "" { effective = override }`. Every subsequent use (`resolved.Branch`) becomes `effective`.

- Naming: `branch` for push (matches `git push origin <branch>`), `head` for create (matches `gh pr create --head`). This mirrors the vocabulary operators already know.
- *Alternative considered:* a required branch with the task default applied at the handler. Rejected — it churns every existing caller and the spec for no benefit; optional-with-default is purely additive.

**2. Safety rails gate the effective branch, not the resolved one.**

Move the default-branch comparison to run against `effective`. The allowlist check is part of `Resolve` (repo-level) and is unaffected. Result: you can target any feature branch in the repo, never `main`/`master`.

- *Alternative considered:* only gate the resolved branch and trust the caller for overrides. Rejected — the whole point of the refusal is to prevent an accidental default-branch push/PR; an override is exactly where a typo would land.

**3. Fork-qualification composes without new code.**

`detectForkUpstream` inspects the repo's `origin`, independent of branch. The fork-qualified head is built as `fu.ForkOwner + ":" + effective` instead of `... + resolved.Branch`. Same-repo head is `effective`. No fork logic changes.

**4. No existence pre-check.**

If the named branch doesn't exist locally (push) or on origin/fork (PR), `git push` / `gh pr create` already return a clear error that iris wraps. Adding a pre-check duplicates git's own validation and risks false negatives. The `branch == ""` "task has no current branch" guard stays for the resolve-from-task path.

## Risks / Trade-offs

- **An override targeting an unpushed/nonexistent branch fails at git/gh, not earlier** → acceptable; the wrapped error names the branch, and a pre-check adds no real safety.
- **Force-with-lease on an arbitrary branch could overwrite a ref** → same risk that already exists for the task branch; `force_with_lease` semantics are unchanged and the lease itself is the guard.
- **Caller could push a branch unrelated to the task** → that is the explicit intent; the allowlist still confines it to the task's repo.

## Migration Plan

Purely additive — no migration. Existing MCP/CLI callers that omit the new parameter see identical behavior. Ship verb + handler + CLI together so the override is reachable from both surfaces.

## Open Questions

None.

## Acceptance criteria

**`iris:push` branch override:**

- it should push the override branch (not the task branch) when `branch` is provided
- it should push the task's resolved branch when `branch` is omitted
- it should refuse when the override branch equals the default branch
- it should return `{pushed, branch, remote_sha}` reporting the branch actually pushed

**`iris:gh_pr_create` head override:**

- it should open the PR for the override head (not the task branch) when `head` is provided
- it should open the PR for the task's resolved branch when `head` is omitted
- it should refuse when the override head equals the default branch
- it should fork-qualify the override head (`<fork-owner>:<head>`) when origin is a fork
