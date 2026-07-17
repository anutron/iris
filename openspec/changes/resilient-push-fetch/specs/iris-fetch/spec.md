## MODIFIED Requirements

### Requirement: `iris:fetch` verb

The plugin SHALL expose `iris:fetch` as an MCP tool and CLI subcommand that runs `git fetch origin` in the resolved source repo and returns the list of refs updated. The verb SHALL accept `task_id` (required, string). On success, the verb SHALL return `{ fetched: true, refs_updated: [...] }`.

The `git fetch` invocation itself SHALL run under a timeout owned by iris, not the caller's context: iris SHALL derive a fresh context via `context.WithTimeout(context.WithoutCancel(ctx), timeout)` before invoking git, so cancellation of the caller-supplied `ctx` (e.g. an inbound MCP request context torn down because argus's client gave up waiting) does NOT terminate an in-flight fetch. `timeout` SHALL be the source repo's configured `git_transfer_timeout_seconds` (see `iris-host-bridge`), or the default when unset. The pre-fetch ref snapshot happens before any mutation is attempted and SHALL continue to run under the caller's context unchanged — if `ctx` is already dead at that point, aborting before fetching is correct. Once the fetch has landed, the post-fetch ref snapshot SHALL also run detached from `ctx` (via the same `context.WithoutCancel` mechanism, under a small fixed grace period rather than the configurable transfer timeout), so a caller's context dying in the same instant the fetch completes cannot turn a genuine success into a reported failure.

On failure, the verb SHALL classify the failure and surface the classification in the returned error's message: `timeout` (iris's own deadline fired before the fetch completed), `auth_failure` (git's output matches a known credential/permission failure pattern), `network_failure` (git's output matches a known connectivity failure pattern), or `other_failure` (any other non-zero git exit).

#### Scenario: Successful fetch returns updated refs

- **WHEN** `iris:fetch` is invoked and origin has new commits on one or more branches
- **THEN** iris runs `git fetch origin` in the resolved source repo and returns `{ fetched: true, refs_updated: [{ ref: "refs/remotes/origin/main", old_sha, new_sha }, ...] }`

#### Scenario: Up-to-date fetch returns empty updates

- **WHEN** `iris:fetch` is invoked and origin has no new refs
- **THEN** iris returns `{ fetched: true, refs_updated: [] }`

#### Scenario: Non-zero git exit returns structured error

- **WHEN** git exits non-zero (network failure, auth failure)
- **THEN** iris returns a structured error carrying git's stderr, classified `auth_failure`, `network_failure`, or `other_failure` per the known-pattern match

#### Scenario: Refuses unknown task ID

- **WHEN** invoked with an unknown `task_id`
- **THEN** iris returns a structured error and does NOT shell out to git

#### Scenario: Refuses non-allowlisted source repo

- **WHEN** the resolved source repo is not on the argus project allowlist
- **THEN** iris returns a structured error naming the rejected path

#### Scenario: Per-source-repo lock held for fetch

- **WHEN** two concurrent `iris:fetch` calls target the same source repo
- **THEN** the second blocks until the first releases the lock

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris fetch <task-id>`
- **THEN** the same `verbs.Fetch` Go function executes and prints the structured result

#### Scenario: Cancelling the caller's context does not kill an in-flight fetch

- **WHEN** the verb is invoked with a `ctx` that is cancelled (e.g. simulating the inbound MCP request context being torn down) while the `git fetch` subprocess is already running
- **THEN** the fetch continues to completion under iris's own detached timeout, is NOT killed by the cancellation, the subsequent post-fetch ref snapshot also runs detached from the cancelled `ctx`, and the verb returns `{fetched: true, ...}` reflecting the completed fetch

#### Scenario: Configured `git_transfer_timeout_seconds` governs how long a fetch may run

- **WHEN** the source repo's `.iris.toml` sets `git_transfer_timeout_seconds` to a value shorter than the time the fetch would otherwise take
- **THEN** iris's own deadline fires at approximately the configured duration (not the default, not immediately) and the verb returns an error classified `timeout`

#### Scenario: Timeout failure is classified distinctly from other failures

- **WHEN** the `git fetch` invocation fails because iris's own configured timeout elapsed
- **THEN** the returned error is a `*verbs.GitTransferError` with `Reason` `timeout`, distinguishable via `verbs.IsGitTransferTimeout(err)`, and its message states plainly that this was iris's own deadline (not a network or auth failure)
