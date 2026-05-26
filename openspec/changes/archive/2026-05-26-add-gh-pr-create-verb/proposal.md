## Why

After `iris:push` lands a task branch on origin, the next move is opening a pull request. Agents inside argus worktrees cannot authenticate to GitHub directly: the sandbox blocks the credentials that gh stores under `~/.config/gh/hosts.yml`. `iris:gh_pr_create` is the host-side verb that closes the gap, shelling out to the host's gh CLI so the agent can request a PR by task ID alone.

## What Changes

- Add `iris:gh_pr_create` verb. Resolves source repo from `task_id`, refuses to open a PR from the default branch, shells out to `gh pr create --base <default> --head <branch> --title <T> [--body <B>] [--draft]` in the source repo, parses the PR number and URL from gh's output.
- Wire MCP handler `iris_gh_pr_create` and CLI subcommand `iris gh-pr-create <task-id> --title T [--body B] [--draft]`.

## Capabilities

### New Capabilities

- `iris-gh-pr-create`: The `iris:gh_pr_create` verb specifically — input contract, default-branch refusal, gh subprocess invocation, structured response with PR number and URL.

### Modified Capabilities

None.

## Impact

- Adds a runtime dependency on `gh` being on the host's PATH and authenticated (`gh auth status` clean). The verb surfaces gh's stderr verbatim when the user is unauthed so the operator gets `gh auth login` as actionable text.
- No new host state, no installer change.
- Narrow surface: no template substitution from commit messages, no reviewers/labels/milestones in v1. The caller supplies title and body.
