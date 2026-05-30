# iris-set-local-config Specification

## Purpose
TBD - created by archiving change add-iris-local-toml-overlay. Update Purpose after archive.
## Requirements
### Requirement: `iris:set_local_config` verb

The plugin SHALL expose `iris:set_local_config` as an MCP tool and CLI subcommand that writes (or merges into) `.iris.local.toml` at a resolved source repo's root with worker-supplied per-developer fields. The verb SHALL refuse any field name whose taxonomy classification is not `local`. The verb SHALL acquire the source-repo lock for the read-modify-write.

Inputs:

- `task_id` (string, optional) — standard resolution semantics.
- `fields` (object, optional) — map of local-tagged field name → value to set. Empty/omitted means no sets.
- `delete` (array, optional) — list of local-tagged field names to remove from the file. Empty/omitted means no deletes. Names not present in the existing file are silently ignored.

Result shape:

- `written` (bool) — `true` if a write occurred (including no-op equality).
- `path` (string) — absolute path to `.iris.local.toml` at the source repo root.
- `resolved` (object) — the merged contents of the file after the write.
- `warnings` (array) — non-fatal warnings.

Merge order: load existing file (treat absent as empty), apply `delete` removals, apply `fields` sets, write back atomically (tmp + rename).

#### Scenario: Sets a local field in a fresh repo

- **GIVEN** a source repo with no `.iris.local.toml` at its root
- **WHEN** `iris:set_local_config` is invoked with `fields = { dogfood_branch: "dev" }`
- **THEN** iris creates `.iris.local.toml` containing `dogfood_branch = "dev"`, returns `{ written: true, path: "<repo>/.iris.local.toml", resolved: { dogfood_branch: "dev" } }`

#### Scenario: Merges with existing file, preserving other fields

- **GIVEN** a source repo with `.iris.local.toml` containing `dogfood_branch = "dev"` and `ship_ci_timeout_seconds = 900`
- **WHEN** `iris:set_local_config` is invoked with `fields = { dogfood_branch: "scratch" }`
- **THEN** the file becomes `dogfood_branch = "scratch"` AND `ship_ci_timeout_seconds = 900`, and the result `resolved` shows both

#### Scenario: Deletes a field

- **GIVEN** a source repo with `.iris.local.toml` containing `dogfood_branch = "dev"` and `ship_ci_timeout_seconds = 900`
- **WHEN** `iris:set_local_config` is invoked with `delete = [ "ship_ci_timeout_seconds" ]`
- **THEN** the file becomes `dogfood_branch = "dev"` only; `resolved` reports `{ dogfood_branch: "dev" }`

#### Scenario: Refuses a shared-tagged field

- **GIVEN** any source repo
- **WHEN** `iris:set_local_config` is invoked with `fields = { default_branch: "trunk" }` (a shared-tagged field)
- **THEN** iris returns an error `{ code: "field_not_local", field: "default_branch", hint: "default_branch is a shared field; edit .iris.toml directly" }` and performs no writes

#### Scenario: Refuses a shared-tagged field in delete

- **WHEN** `iris:set_local_config` is invoked with `delete = [ "build" ]` (shared)
- **THEN** iris returns the same `field_not_local` error and performs no writes

#### Scenario: Refuses unknown field

- **WHEN** `iris:set_local_config` is invoked with `fields = { dogfodo_brnach: "dev" }` (typo, not a known field)
- **THEN** iris returns an error `{ code: "unknown_field", field: "dogfodo_brnach", hint: "valid local fields: dogfood_branch, ship_ci_timeout_seconds" }` and performs no writes

#### Scenario: Validates per-field rules

- **WHEN** `iris:set_local_config` is invoked with `fields = { dogfood_branch: "no spaces allowed" }` (invalid git ref)
- **THEN** iris returns an error `{ code: "invalid_value", field: "dogfood_branch", message: "invalid git branch name", hint: "use a single ref name without spaces or invalid characters" }` and performs no writes

#### Scenario: Refuses dogfood_branch equal to default_branch

- **GIVEN** a source repo whose default branch is `main`
- **WHEN** `iris:set_local_config` is invoked with `fields = { dogfood_branch: "main" }`
- **THEN** iris returns an error citing the same rule as `validate_config` (`dogfood_branch SHALL NOT equal default_branch`)

#### Scenario: Atomic write (tmp + rename)

- **WHEN** `iris:set_local_config` writes the file
- **THEN** iris writes to a temporary path next to `.iris.local.toml` and renames atomically, so a crash mid-write does NOT leave a partially-written file

#### Scenario: Idempotent re-set

- **GIVEN** a source repo with `.iris.local.toml` already containing `dogfood_branch = "dev"`
- **WHEN** `iris:set_local_config` is invoked with `fields = { dogfood_branch: "dev" }` (same value)
- **THEN** iris returns `{ written: true, ... }` and the file is rewritten with identical contents (no error)

#### Scenario: Concurrent set_local_config calls serialize via the source-repo lock

- **GIVEN** two simultaneous `iris:set_local_config` invocations targeting the same source repo
- **WHEN** both are dispatched
- **THEN** they serialize through the existing per-source-repo mutex; the second sees the first's writes when it loads the existing file

#### Scenario: Both fields and delete in one call

- **GIVEN** a source repo with `.iris.local.toml` containing `dogfood_branch = "dev"` and `ship_ci_timeout_seconds = 900`
- **WHEN** `iris:set_local_config` is invoked with `fields = { dogfood_branch: "scratch" }` AND `delete = [ "ship_ci_timeout_seconds" ]`
- **THEN** the file becomes `dogfood_branch = "scratch"` (no ship_ci_timeout_seconds); resolved reflects this

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris set-local-config --field dogfood_branch=dev` from any shell
- **THEN** the same `verbs.SetLocalConfig` Go function executes and prints the structured result as pretty-printed JSON

