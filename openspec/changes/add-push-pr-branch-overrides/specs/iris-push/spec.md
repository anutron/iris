# iris-push Specification

## MODIFIED Requirements

### Requirement: `iris:push` verb

The plugin SHALL expose `iris:push` as an MCP tool accepting `task_id` (string, required), `force_with_lease` (bool, default false), and `branch` (string, optional). When `branch` is provided and non-empty, the verb SHALL push that branch; when `branch` is omitted or empty, the verb SHALL push the task's resolved branch. The branch actually acted upon is the "effective branch". On success the verb SHALL return `{pushed: true, branch, remote_sha}` where `branch` is the effective branch; on failure it SHALL return a structured error and leave origin's refs unchanged. The default-branch refusal and source-repo allowlist SHALL apply to the effective branch.

#### Scenario: Successful push of a task branch

- **WHEN** the verb is invoked without `branch` for a task whose branch `argus/<task-slug>` has commits ahead of origin
- **THEN** iris runs `git push origin argus/<task-slug>` in the source repo, reads `git rev-parse origin/argus/<task-slug>` for the resulting remote SHA, and returns `{pushed: true, branch: "argus/<task-slug>", remote_sha: "<sha>"}`

#### Scenario: Explicit branch override pushes the named branch

- **WHEN** the verb is invoked with `branch="feature-x"` for a task whose resolved branch is `argus/<task-slug>`, and `feature-x` exists in the source repo with commits ahead of origin
- **THEN** iris runs `git push origin feature-x` in the source repo (NOT the resolved task branch), and returns `{pushed: true, branch: "feature-x", remote_sha: "<sha>"}`

#### Scenario: Refuses to push the default branch

- **WHEN** the effective branch (resolved task branch, or the `branch` override) equals the source repo's default branch (`main` or `master`)
- **THEN** iris returns a structured error and performs no push (origin's refs are unchanged)

#### Scenario: Refuses an unknown task ID

- **WHEN** the verb is invoked with a `task_id` that argus does not recognize
- **THEN** iris returns a structured error naming the task ID and performs no push

#### Scenario: `--force-with-lease` succeeds when remote tracks correctly

- **WHEN** the verb is invoked with `force_with_lease=true` after a `git commit --amend` on the effective branch
- **THEN** iris runs `git push origin <effective-branch> --force-with-lease`, the push succeeds, and origin's ref advances to the amended SHA

#### Scenario: Non-fast-forward without `--force-with-lease` errors clearly

- **WHEN** origin's effective branch has a commit the worktree does not, and the verb is invoked with `force_with_lease=false`
- **THEN** iris returns a structured error surfacing git's non-fast-forward message and origin's ref remains at the competing commit

#### Scenario: Refuses a source repo outside the project allowlist

- **WHEN** the resolved source-repo path does not match any allowlisted argus project
- **THEN** iris returns a structured error naming the rejected path and performs no push

#### Scenario: Direct CLI invocation runs the same verb

- **WHEN** the user runs `iris push <task-id> [--force-with-lease] [--branch <name>]` from any shell on the host
- **THEN** the same `verbs.Push` Go function executes (bypassing the daemon process) and prints the structured result
