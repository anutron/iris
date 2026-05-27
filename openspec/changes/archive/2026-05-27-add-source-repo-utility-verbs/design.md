# Design: source-repo utility verbs

## Context

v1.0 ships two git-mutating verbs (`merge_to_master`, `push`) plus the gh-shelling pair. Three more git operations fit cleanly within the same shape: fetch, remote-branch-delete, tag-and-push. Each is a small, atomic, host-side operation that an argus-sandboxed agent cannot perform directly (ssh keys for origin live on the host).

This change is mechanical: three new verbs following the v1.0 git-verb pattern.

## Goals

- Round out the realistic release-shipping surface (fetch latest origin state, clean up a merged branch, cut a release tag).
- Reuse the v1.0 patterns: typed inputs, per-source-repo lock, structured results, real `git` against tempdirs for tests with a bare origin.

## Non-goals

- No `iris:reset` (destructive, no current workflow).
- No `iris:rebase` (overlaps with `merge_to_master` for the "ship to main" path).
- No `iris:cherry_pick` (agent can do this in its own worktree).
- No tag deletion verb in v1.1; tags are append-only by convention.

## Decisions

### D1. Fetch fetches origin only

`iris:fetch` calls `git fetch origin` (not `--all`). v1.0 verbs treat origin as the canonical remote; staying consistent. If a future workflow needs `--all`, it lands as an opt-in flag.

### D2. Branch-delete refuses the default branch

`iris:branch_delete_remote` resolves the default branch via `git symbolic-ref refs/remotes/origin/HEAD` and refuses if the requested branch matches. Matches the existing v1.0 `iris:push` invariant of refusing default-branch operations.

### D3. Tag is annotated by default, push is included

`iris:tag` creates a `git tag -a <name> -m <message> origin/<default-branch>` (annotated, at the SHA on origin's default branch) and then `git push origin <name>`. Annotated tags are correct for releases; lightweight tags are out of scope.

### D4. Tag refuses existing names

If `git rev-parse <tag>` succeeds locally OR `git ls-remote --tags origin <tag>` returns a hit, iris refuses. No clobber.

### D5. Lock scope is the full git operation

Each verb holds the per-source-repo lock from the moment after `verbs.Resolve` returns until git's process exits. Standard v1.0 pattern.

## Acceptance criteria

### `iris:fetch`

- it should run `git fetch origin` in the resolved source repo, returning the list of refs updated and their new SHAs
- it should refuse with a structured error on non-zero git exit, carrying stderr
- it should refuse when the resolved source repo is not on the argus project allowlist
- it should hold the per-source-repo lock for the duration of the fetch
- it should mirror the same Go function from CLI invocation (`iris fetch <task-id>`)

### `iris:branch_delete_remote`

- it should refuse when `branch` equals the resolved default branch, naming both
- it should refuse when `branch` is empty
- it should refuse when the branch does not exist on origin (best-effort pre-check via `git ls-remote --heads origin <branch>`)
- it should run `git push origin :<branch>` and return `{ deleted: true, branch, prior_remote_sha }`
- it should refuse on non-zero git exit with a structured error
- it should hold the per-source-repo lock for the duration of the push
- it should mirror the same Go function from CLI invocation

### `iris:tag`

- it should refuse when `tag` is empty
- it should refuse when `tag` already exists locally or on origin, naming the conflict
- it should create an annotated tag at the SHA of `origin/<default-branch>` with the configured `message` (or a default `"Released by iris"` when empty)
- it should push the tag to origin and return `{ tagged: true, tag, sha, message }`
- it should refuse on non-zero git exit with a structured error containing git's output
- it should hold the per-source-repo lock for the duration of the tag-create + push
- it should mirror the same Go function from CLI invocation

## Risks / Trade-offs

- **Risk:** A concurrent push during fetch causes git's refspec output parsing to drift. → Mitigation: parsing is best-effort; iris returns the raw output in the result when parsing fails.
- **Risk:** Tag push to a protected remote rejects the tag. → Mitigation: iris returns the structured error from git verbatim; the caller decides whether to retry, force, or abandon. No force-push of tags in v1.1.
- **Trade-off:** Three small verbs is repetitive code-wise but each is independently testable. Matches v1.0 ergonomics.

## Migration plan

1. Ship on the same feature branch as `add-daemon-self-management` and `add-pr-management-verbs`.
2. Dogfood by using `iris:fetch` before the v1.1 archive PR merge to verify the local default branch is up to date.
3. After dogfood, archive.
4. Rollback: revert. No state to clean up.

## Open questions

None.
