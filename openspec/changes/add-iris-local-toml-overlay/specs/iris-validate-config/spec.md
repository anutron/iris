## ADDED Requirements

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
