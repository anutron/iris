# iris-ls Specification

## ADDED Requirements

### Requirement: `iris:ls` verb

The plugin SHALL expose `iris:ls` as an MCP tool and CLI subcommand that reads `~/.iris/reload-history.jsonl` and returns a list of managed systems iris has reloaded recently, with aggregate counts and last-reload metadata. The verb SHALL NOT maintain a separate registry; the audit log is the source of truth.

#### Scenario: Lists managed systems sorted by recency

- **GIVEN** `~/.iris/reload-history.jsonl` contains audit entries for source repos A (5 reloads, latest 1h ago), B (1 reload, latest 1d ago), and C (3 reloads, latest 1m ago)
- **WHEN** `iris:ls` is invoked with no filter
- **THEN** iris returns a list ordered [C, A, B] (descending by `last_reload_at`), each entry containing `{ source_repo, last_reload_at, last_outcome, last_mode, last_pre_pull_sha, last_post_pull_sha, total_reload_count, total_failure_count }`

#### Scenario: Empty audit log returns empty list

- **WHEN** `~/.iris/reload-history.jsonl` does not exist or contains zero lines
- **THEN** iris returns `{ entries: [], warnings: ["no reloads recorded yet"] }`

#### Scenario: Limit caps the result count

- **WHEN** `iris:ls` is invoked with `limit = 10` and the audit log contains 50 distinct source repos
- **THEN** iris returns the 10 most recent entries (after dedup)

#### Scenario: Since filter excludes older entries

- **WHEN** `iris:ls` is invoked with `since = "2026-05-26T00:00:00Z"`
- **THEN** iris excludes any audit entry whose `timestamp` precedes that ISO 8601 / RFC 3339 value

#### Scenario: No side effects

- **WHEN** `iris:ls` is invoked for any inputs
- **THEN** iris performs no source-repo lock acquisition, no `git` invocation, no audit-log write — only an audit-log read

#### Scenario: `task_id` is not required

- **WHEN** `iris:ls` is invoked with no `task_id`
- **THEN** iris returns the full system inventory (this verb is global, not target-scoped)

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris ls [--limit=N] [--since=TIMESTAMP]` from any shell
- **THEN** the same `verbs.Ls` Go function executes and prints the structured result as pretty-printed JSON
