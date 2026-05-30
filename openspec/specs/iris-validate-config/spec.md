# iris-validate-config Specification

## Purpose
TBD - created by archiving change add-daemon-self-management. Update Purpose after archive.
## Requirements
### Requirement: `iris:validate_config` verb

The plugin SHALL expose `iris:validate_config` as an MCP tool and CLI subcommand that parses and cross-validates a `.iris.toml` file at a resolved source repo without performing any side effects. The verb SHALL use the same parser and cross-validator as `iris:reload`'s pre-flight, and SHALL return a structured `valid: true|false` result with per-error remediation hints.

#### Scenario: Valid config returns valid=true with resolved schema

- **GIVEN** a source repo with `.iris.toml` declaring `schema_version = 1`, valid `[build]` and `[restart]` blocks, and no field conflicts
- **WHEN** `iris:validate_config` is invoked targeting that repo
- **THEN** iris returns `{ valid: true, errors: [], warnings: [...], resolved: { schema_version, default_branch, build, restart, pre_flight?, verify? } }` and performs no pull, build, restart, or audit-log write

#### Scenario: Missing .iris.toml is invalid

- **GIVEN** a source repo with no `.iris.toml` at the repo root
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `{ valid: false, errors: [{ field: ".iris.toml", message: "file not found at <path>", hint: "create .iris.toml at the repo root" }] }`

#### Scenario: Malformed TOML reports line numbers when available

- **GIVEN** a `.iris.toml` with a TOML syntax error
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: false` with an error containing the TOML parser's reported line number and column when present

#### Scenario: Cross-mechanism field conflict surfaces remediation hint

- **GIVEN** a `.iris.toml` with `[restart] mechanism = "launchagent"` and an extra `pid_file = "..."` field
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: false` with an error naming the conflicting field, the declared mechanism, and a hint to either change the mechanism or remove the field

#### Scenario: `task_id` is optional, same shape as reload

- **WHEN** `iris:validate_config` is invoked with no `task_id` and no `path`
- **THEN** iris discovers iris's own source repo via `os.Executable()` and validates iris's own `.iris.toml`

#### Scenario: No side effects

- **WHEN** `iris:validate_config` is invoked for any inputs
- **THEN** iris performs no `git fetch`, `git merge`, build, restart, or audit-log write

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris validate-config [target]` from any shell
- **THEN** the same `verbs.ValidateConfig` Go function executes and prints the structured result as pretty-printed JSON

### Requirement: `iris:validate_config` validates `dogfood_branch`

The `iris:validate_config` verb SHALL validate the new `dogfood_branch` field in `.iris.toml` when present. Unset or empty SHALL be valid (the field is optional and opt-in). A non-empty value SHALL be a syntactically valid git branch name (per `git check-ref-format --branch`). The verb SHALL also validate the optional `ship_ci_timeout_seconds` field as a non-negative integer when present.

#### Scenario: Missing dogfood_branch is valid

- **GIVEN** a `.iris.toml` with no `dogfood_branch` field
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: true` with no error or warning relating to the dogfood branch

#### Scenario: Valid dogfood_branch passes

- **GIVEN** a `.iris.toml` with `dogfood_branch = "dev"`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: true` and the resolved config includes `dogfood_branch: "dev"`

#### Scenario: Invalid branch name reports a remediation hint

- **GIVEN** a `.iris.toml` with `dogfood_branch = "no spaces allowed"` (or any value rejected by `git check-ref-format --branch`)
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: false` with an error of the form `{ field: "dogfood_branch", message: "invalid git branch name", hint: "use a single ref name without spaces or invalid characters" }`

#### Scenario: dogfood_branch equal to default_branch is invalid

- **GIVEN** a `.iris.toml` with `dogfood_branch = "main"` and the source repo's default branch also `main`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: false` with an error explaining that the dogfood branch SHALL NOT equal the default branch and a hint recommending a distinct name like `dev`

#### Scenario: Negative ship_ci_timeout_seconds is invalid

- **GIVEN** a `.iris.toml` with `ship_ci_timeout_seconds = -1`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: false` with an error naming the field and the non-negativity rule

### Requirement: Optional `.iris.local.toml` overlay file

Iris's TOML loader SHALL read `.iris.toml` at the source repo root, then OPTIONALLY read `.iris.local.toml` at the same root, then merge the two into a single resolved config. The local file is per-developer and is expected to be gitignored. The local file is OPTIONAL — its absence is silent and not an error.

When the same field is present in both files, the local file's value SHALL win for fields tagged `local`. For fields tagged `shared`, the local file's value SHALL be ignored and a `shared_field_in_local_config` warning SHALL be appended.

#### Scenario: Local file absent is valid

- **GIVEN** a source repo with `.iris.toml` only (no `.iris.local.toml`)
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: true` with the resolved config from `.iris.toml` and no warning about the missing local file

#### Scenario: Local file overlays a local-tagged field

- **GIVEN** a source repo with `.iris.toml` containing no `dogfood_branch` and `.iris.local.toml` containing `dogfood_branch = "dev"`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: true` with `resolved.dogfood_branch = "dev"`

#### Scenario: Local file overlays a local-tagged field defined in shared (legacy)

- **GIVEN** a source repo with `.iris.toml` containing `dogfood_branch = "shared-default"` and `.iris.local.toml` containing `dogfood_branch = "dev"`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: true` with `resolved.dogfood_branch = "dev"` and one warning each: `local_field_in_shared_config` (for `dogfood_branch` in `.iris.toml`) and no second warning (the override is the intended migration path)

#### Scenario: Malformed local file surfaces a structured error

- **GIVEN** a source repo with valid `.iris.toml` and a `.iris.local.toml` that fails TOML parsing
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: false` with a structured error naming the local file and the parser's line/column when available; `.iris.toml`'s fields are NOT silently lost

#### Scenario: Local file is silent on absent

- **GIVEN** a source repo with valid `.iris.toml` and no `.iris.local.toml`
- **WHEN** `iris:validate_config` is invoked
- **THEN** the result includes NO warning of the form "no local file"

### Requirement: Field taxonomy enforcement

Every field in iris's TOML schema SHALL be tagged either `shared` (project-wide, expected in `.iris.toml`) or `local` (per-developer, expected in `.iris.local.toml`). `iris:validate_config` SHALL warn when a tagged field appears in the wrong file.

Initial taxonomy:

- **Shared**: `schema_version`, `default_branch`, `build`, `restart`, `pre_flight`, `verify`, `post_merge`.
- **Local**: `dogfood_branch`, `ship_ci_timeout_seconds`.

A `local_field_in_shared_config` warning SHALL be appended for each local-tagged field present in `.iris.toml`. The field's value SHALL still be honored (graceful migration), but the warning text SHALL include an explicit migration hint: `"move <field> to .iris.local.toml"`.

A `shared_field_in_local_config` warning SHALL be appended for each shared-tagged field present in `.iris.local.toml`. The shared file's value SHALL be used (the local file's value SHALL be ignored).

#### Scenario: dogfood_branch in .iris.toml warns with migration hint

- **GIVEN** a source repo with `.iris.toml` containing `dogfood_branch = "dev"` and no `.iris.local.toml`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: true`, `resolved.dogfood_branch = "dev"`, and a warning `{ code: "local_field_in_shared_config", field: "dogfood_branch", file: ".iris.toml", hint: "move dogfood_branch to .iris.local.toml" }`

#### Scenario: default_branch in .iris.local.toml warns and is ignored

- **GIVEN** a source repo with `.iris.toml` containing `default_branch = "main"` and `.iris.local.toml` containing `default_branch = "trunk"`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: true`, `resolved.default_branch = "main"` (from shared), and a warning `{ code: "shared_field_in_local_config", field: "default_branch", file: ".iris.local.toml" }`

#### Scenario: ship_ci_timeout_seconds in .iris.toml warns with migration hint

- **GIVEN** a source repo with `.iris.toml` containing `ship_ci_timeout_seconds = 900` and no `.iris.local.toml`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: true`, `resolved.ship_ci_timeout_seconds = 900`, and a warning `{ code: "local_field_in_shared_config", field: "ship_ci_timeout_seconds", file: ".iris.toml", hint: "move ship_ci_timeout_seconds to .iris.local.toml" }`

#### Scenario: Build block in .iris.local.toml warns and is ignored

- **GIVEN** a source repo with `.iris.toml` containing `[build] command = ["make","build"]` and `.iris.local.toml` containing `[build] command = ["echo","local"]`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: true`, `resolved.build.command = ["make","build"]`, and a warning `{ code: "shared_field_in_local_config", field: "build", file: ".iris.local.toml" }`

