# iris-status Specification

## Purpose
TBD - created by archiving change add-daemon-self-management. Update Purpose after archive.
## Requirements
### Requirement: `iris:status` verb

The plugin SHALL expose `iris:status` as an MCP tool and CLI subcommand that, for one managed system, returns the resolved `.iris.toml`, the current git state, and the most recent reload outcome from the audit log. The verb SHALL NOT perform any side effects.

#### Scenario: Returns full status for a managed system

- **GIVEN** a source repo with `.iris.toml`, HEAD at SHA `aaa...`, `origin/main` at SHA `aaa...`, working tree clean, and an audit-log entry from 1h ago with `outcome: "success"` and `post_pull_sha: "aaa..."`
- **WHEN** `iris:status` is invoked targeting that repo
- **THEN** iris returns `{ source_repo, head_sha, default_branch, origin_default_sha, working_tree_clean: true, drift: false, up_to_date: true, config: { schema_version, build, restart, ... }, last_reload: { timestamp, outcome, mode, pre_pull_sha, post_pull_sha, ... } }`

#### Scenario: Drift is reported when local HEAD differs from last reload

- **WHEN** the source repo's HEAD differs from the most recent audit entry's `post_pull_sha`
- **THEN** iris sets `drift: true` in the result

#### Scenario: Not-up-to-date when origin has new commits

- **WHEN** the source repo's HEAD differs from `origin/<default-branch>`
- **THEN** iris sets `up_to_date: false`

#### Scenario: Missing audit log entry returns last_reload null

- **WHEN** no audit-log entry exists for the resolved source repo
- **THEN** iris returns `last_reload: null` and a structured warning ("no reload recorded for this system yet")

#### Scenario: Missing or invalid `.iris.toml` is surfaced but does not fail

- **GIVEN** a source repo with no `.iris.toml` (or a malformed one)
- **WHEN** `iris:status` is invoked
- **THEN** iris returns `config: null` and includes the parse error in `warnings`; the git state and audit log are still reported

#### Scenario: `task_id` is optional, same shape as reload

- **WHEN** `iris:status` is invoked with no `task_id` and no `path`
- **THEN** iris discovers iris's own source repo via `os.Executable()` and reports status on iris itself

#### Scenario: No side effects

- **WHEN** `iris:status` is invoked for any inputs
- **THEN** iris performs no `git fetch`, `git merge`, build, restart, or audit-log write

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris status [target]` from any shell
- **THEN** the same `verbs.Status` Go function executes and prints the structured result as pretty-printed JSON

