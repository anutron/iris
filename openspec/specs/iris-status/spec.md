# iris-status Specification

## Purpose
TBD - created by archiving change add-daemon-self-management. Update Purpose after archive.
## Requirements
### Requirement: `iris:status` verb

The plugin SHALL expose `iris:status` as an MCP tool and CLI subcommand that, for one managed system, returns the resolved `.iris.toml`, the current git state, the most recent reload outcome from the audit log, and (when discoverable) the matching argus task. The verb SHALL NOT perform any side effects.

The result shape adds:

- `branch` (string) – the current HEAD branch name from `git rev-parse --abbrev-ref HEAD`, or empty when HEAD is detached.
- `argus_task` (`*argus.Task` or null) – populated when iris can find an argus task whose canonicalized `worktree_path` equals the resolved source repo. Null when no match exists, or when iris cannot reach argus.

The result shape changes for missing `.iris.toml`:

- A missing file produces `config: null` and NO warning. Parse errors and validation errors still surface as warnings, unchanged.

#### Scenario: Returns branch and argus_task when available

- **GIVEN** a source repo with `.iris.toml`, HEAD on branch `argus/feature-x`, and an argus task with `worktree_path` equal to that source repo
- **WHEN** `iris:status` is invoked targeting that repo
- **THEN** iris returns the existing fields plus `branch: "argus/feature-x"` and `argus_task: { id, name, project, status, worktree_path, branch }`

#### Scenario: Branch reports detached HEAD as empty

- **WHEN** the source repo's HEAD is detached
- **THEN** iris returns `branch: ""` and the rest of the result unchanged

#### Scenario: argus_task is null when no task matches

- **GIVEN** a source repo with no argus task pointing at it (argus reachable, list returned)
- **WHEN** `iris:status` is invoked
- **THEN** iris returns `argus_task: null` with NO argus-related warning

#### Scenario: argus unreachable surfaces a warning

- **GIVEN** argus returns an error to `GET /api/tasks` (or is unreachable)
- **WHEN** `iris:status` is invoked
- **THEN** iris returns `argus_task: null` and appends a warning of the form "could not query argus for matching task: <err>" to `warnings`. The verb does NOT fail.

#### Scenario: Missing `.iris.toml` is silent

- **GIVEN** a source repo with no `.iris.toml`
- **WHEN** `iris:status` is invoked
- **THEN** iris returns `config: null` with no warning about the missing file; git state and audit log are still reported

#### Scenario: Malformed `.iris.toml` still warns

- **GIVEN** a source repo with a `.iris.toml` that fails TOML parsing
- **WHEN** `iris:status` is invoked
- **THEN** iris returns `config: null` and includes the parse error in `warnings`

#### Scenario: Drift is reported when local HEAD differs from last reload

- **WHEN** the source repo's HEAD differs from the most recent audit entry's `post_pull_sha`
- **THEN** iris sets `drift: true` in the result

#### Scenario: Not-up-to-date when origin has new commits

- **WHEN** the source repo's HEAD differs from `origin/<default-branch>`
- **THEN** iris sets `up_to_date: false`

#### Scenario: Missing audit log entry returns last_reload null

- **WHEN** no audit-log entry exists for the resolved source repo
- **THEN** iris returns `last_reload: null` and a structured warning ("no reload recorded for this system yet")

#### Scenario: `task_id` is optional, same shape as reload

- **WHEN** `iris:status` is invoked with no `task_id` and no `path`
- **THEN** iris discovers iris's own source repo via `os.Executable()` and reports status on iris itself

#### Scenario: No side effects

- **WHEN** `iris:status` is invoked for any inputs
- **THEN** iris performs no `git fetch`, `git merge`, build, restart, or audit-log write

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris status [target]` from any shell
- **THEN** the same `verbs.Status` Go function executes and prints the structured result as pretty-printed JSON

### Requirement: `iris:status` includes dogfood manifest

The `iris:status` verb SHALL include the current dogfood manifest in its result whenever a `dogfood-manifest.json` file exists for the source repo. When no such file exists, the field SHALL be `null`. The verb SHALL NOT fail if the manifest file is malformed; instead it SHALL set the field to `null` and append a structured warning.

The added result field:

- `dogfood` (object or null) — the persisted manifest including `recorded_at`, or `null` if absent/malformed.

#### Scenario: Returns dogfood manifest when present

- **GIVEN** a source repo with a valid `dogfood-manifest.json` containing `base`, `layered`, and `recorded_at`
- **WHEN** `iris:status` is invoked
- **THEN** iris returns the existing fields plus `dogfood: { base: {...}, layered: [...], note?: "...", recorded_at: "..." }`

#### Scenario: Returns dogfood null when no manifest exists

- **GIVEN** a source repo with no `dogfood-manifest.json` file
- **WHEN** `iris:status` is invoked
- **THEN** iris returns `dogfood: null` with no warning

#### Scenario: Malformed manifest reports null with a warning

- **GIVEN** a source repo with a `dogfood-manifest.json` that fails JSON parsing or schema validation
- **WHEN** `iris:status` is invoked
- **THEN** iris returns `dogfood: null` and appends a structured warning identifying the manifest path and the parse/validation error

#### Scenario: Manifest reporting has no side effects

- **WHEN** `iris:status` is invoked for any inputs
- **THEN** iris does NOT modify the manifest file, the dogfood branch, or any git refs

