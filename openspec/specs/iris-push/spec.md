# iris-push Specification

## Purpose
TBD - created by archiving change add-push-verb. Update Purpose after archive.
## Requirements
### Requirement: `iris:push` verb

The plugin SHALL expose `iris:push` as an MCP tool accepting `task_id` (string, required), `force_with_lease` (bool, default false), `branch` (string, optional), and `remote` (string, optional). When `branch` is provided and non-empty, the verb SHALL push that branch; when `branch` is omitted or empty, the verb SHALL push the task's resolved branch. The branch actually acted upon is the "effective branch". When `remote` is provided and non-empty, the verb SHALL push to that named git remote; when `remote` is omitted or empty, the verb SHALL push to `origin`. The remote actually acted upon is the "effective remote". The effective remote MUST be a remote already configured in the source repo — iris SHALL validate it exists (e.g. via `git remote get-url <remote>`) before pushing and SHALL NOT accept a URL or add remotes. On success the verb SHALL return `{pushed: true, branch, remote, remote_sha}` where `branch` is the effective branch and `remote` is the effective remote; on failure it SHALL return a structured error and leave the remote's refs unchanged. The default-branch refusal and source-repo allowlist SHALL apply to the effective branch. The verb SHALL reject an effective branch or an effective remote that begins with `-` before invoking git, so an override cannot smuggle flags into `git push`.

#### Scenario: Successful push of a task branch

- **WHEN** the verb is invoked without `branch` or `remote` for a task whose branch `argus/<task-slug>` has commits ahead of origin
- **THEN** iris runs `git push origin argus/<task-slug>` in the source repo, reads `git rev-parse origin/argus/<task-slug>` for the resulting remote SHA, and returns `{pushed: true, branch: "argus/<task-slug>", remote: "origin", remote_sha: "<sha>"}`

#### Scenario: Explicit branch override pushes the named branch

- **WHEN** the verb is invoked with `branch="feature-x"` for a task whose resolved branch is `argus/<task-slug>`, and `feature-x` exists in the source repo with commits ahead of origin
- **THEN** iris runs `git push origin feature-x` in the source repo (NOT the resolved task branch), and returns `{pushed: true, branch: "feature-x", remote: "origin", remote_sha: "<sha>"}`

#### Scenario: Explicit remote override pushes to the named remote

- **WHEN** the verb is invoked with `remote="upstream"` for a task whose effective branch is `argus/<task-slug>`, and `upstream` is a remote configured in the source repo
- **THEN** iris runs `git push upstream argus/<task-slug>` (NOT origin), reads `git rev-parse upstream/argus/<task-slug>`, and returns `{pushed: true, branch: "argus/<task-slug>", remote: "upstream", remote_sha: "<sha>"}`

#### Scenario: Refuses a remote that is not configured

- **WHEN** the verb is invoked with a `remote` that is not a remote configured in the source repo
- **THEN** iris returns a structured error naming the unknown remote, does NOT invoke `git push`, and no ref changes

#### Scenario: Rejects a remote override beginning with a dash

- **WHEN** the verb is invoked with a `remote` override that begins with `-`
- **THEN** iris returns a structured error stating the remote must not begin with `-`, does NOT invoke git, and no ref changes

#### Scenario: Rejects a branch override beginning with a dash

- **WHEN** the verb is invoked with a `branch` override that begins with `-` (e.g. `--upload-pack=evil`)
- **THEN** iris returns a structured error stating the branch must not begin with `-`, does NOT invoke git, and origin's refs are unchanged

#### Scenario: Refuses to push the default branch

- **WHEN** the effective branch (resolved task branch, or the `branch` override) equals the source repo's default branch (`main` or `master`)
- **THEN** iris returns a structured error and performs no push (the remote's refs are unchanged)

#### Scenario: Refuses an unknown task ID

- **WHEN** the verb is invoked with a `task_id` that argus does not recognize
- **THEN** iris returns a structured error naming the task ID and performs no push

#### Scenario: `--force-with-lease` succeeds when remote tracks correctly

- **WHEN** the verb is invoked with `force_with_lease=true` after a `git commit --amend` on the effective branch
- **THEN** iris runs `git push <effective-remote> <effective-branch> --force-with-lease`, the push succeeds, and the remote's ref advances to the amended SHA

#### Scenario: Non-fast-forward without `--force-with-lease` errors clearly

- **WHEN** the effective remote's branch has a commit the worktree does not, and the verb is invoked with `force_with_lease=false`
- **THEN** iris returns a structured error surfacing git's non-fast-forward message and the remote's ref remains at the competing commit

#### Scenario: Refuses a source repo outside the project allowlist

- **WHEN** the resolved source-repo path does not match any allowlisted argus project
- **THEN** iris returns a structured error naming the rejected path and performs no push

#### Scenario: Direct CLI invocation runs the same verb

- **WHEN** the user runs `iris push <task-id> [--force-with-lease] [--branch <name>] [--remote <name>]` from any shell on the host
- **THEN** the same `verbs.Push` Go function executes (bypassing the daemon process) and prints the structured result

