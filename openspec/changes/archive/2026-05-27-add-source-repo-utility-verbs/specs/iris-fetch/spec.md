# iris-fetch Specification

## ADDED Requirements

### Requirement: `iris:fetch` verb

The plugin SHALL expose `iris:fetch` as an MCP tool and CLI subcommand that runs `git fetch origin` in the resolved source repo and returns the list of refs updated. The verb SHALL accept `task_id` (required, string). On success, the verb SHALL return `{ fetched: true, refs_updated: [...] }`.

#### Scenario: Successful fetch returns updated refs

- **WHEN** `iris:fetch` is invoked and origin has new commits on one or more branches
- **THEN** iris runs `git fetch origin` in the resolved source repo and returns `{ fetched: true, refs_updated: [{ ref: "refs/remotes/origin/main", old_sha, new_sha }, ...] }`

#### Scenario: Up-to-date fetch returns empty updates

- **WHEN** `iris:fetch` is invoked and origin has no new refs
- **THEN** iris returns `{ fetched: true, refs_updated: [] }`

#### Scenario: Non-zero git exit returns structured error

- **WHEN** git exits non-zero (network failure, auth failure)
- **THEN** iris returns a structured error carrying git's stderr

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
