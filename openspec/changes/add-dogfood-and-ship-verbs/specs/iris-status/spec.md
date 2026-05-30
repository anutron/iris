## ADDED Requirements

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
