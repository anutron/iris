## ADDED Requirements

### Requirement: `iris:status` reports config_sources

The `iris:status` verb SHALL include a `config_sources` field in its result, mapping each populated config field name to the file it was sourced from (`"shared"` for `.iris.toml`, `"local"` for `.iris.local.toml`). Fields that were not set in either file SHALL be omitted from `config_sources`.

The mapping covers top-level scalar fields and the names of top-level table blocks (e.g., `"build"`, `"restart"`), not their nested fields.

#### Scenario: config_sources reports shared and local provenance

- **GIVEN** a source repo with `.iris.toml` setting `default_branch`, `[build]`, `[restart]` and `.iris.local.toml` setting `dogfood_branch`
- **WHEN** `iris:status` is invoked
- **THEN** the result includes `config_sources: { default_branch: "shared", build: "shared", restart: "shared", dogfood_branch: "local" }`

#### Scenario: config_sources omits fields set in neither file

- **GIVEN** a source repo where `ship_ci_timeout_seconds` is unset in both files
- **WHEN** `iris:status` is invoked
- **THEN** `config_sources` does NOT contain a `ship_ci_timeout_seconds` key

#### Scenario: config_sources reports migration state

- **GIVEN** a source repo with `.iris.toml` containing `dogfood_branch = "dev"` (legacy placement) and no `.iris.local.toml`
- **WHEN** `iris:status` is invoked
- **THEN** `config_sources` contains `dogfood_branch: "shared"`, allowing a human or agent to see the field is still in the un-migrated location

#### Scenario: config_sources absent when no config loaded

- **GIVEN** a source repo with no `.iris.toml` and no `.iris.local.toml`
- **WHEN** `iris:status` is invoked
- **THEN** the result has `config: null` and `config_sources: {}` (empty object, not absent), so consumers can rely on the field always being an object
