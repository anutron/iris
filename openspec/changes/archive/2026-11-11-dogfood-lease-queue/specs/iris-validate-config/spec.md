## ADDED Requirements

### Requirement: `iris:validate_config` validates `[dogfood_lease]`

The `iris:validate_config` verb SHALL validate the optional `[dogfood_lease]` block when it is present in the merged configuration. The block SHALL be tagged `local` in the field taxonomy. When it appears in `.iris.toml`, iris SHALL append a `local_field_in_shared_config` warning with the hint `"move dogfood_lease to .iris.local.toml"`, and SHALL still honor the value.

Validation rules:

- `warmer_seconds`, `grace_seconds` and `idle_release_seconds` SHALL each be a positive integer when present.
- After defaults are applied (600, 30 and 1800), the effective values SHALL satisfy `grace_seconds < warmer_seconds <= idle_release_seconds`.

The resolved config SHALL report the effective timer values.

#### Scenario: Missing block is valid and resolves to defaults

- **GIVEN** a merged config with no `[dogfood_lease]` block
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: true`, and the resolved config reports `dogfood_lease: { warmer_seconds: 600, grace_seconds: 30, idle_release_seconds: 1800 }`

#### Scenario: Non-positive timer is invalid

- **GIVEN** `.iris.local.toml` declares `[dogfood_lease] grace_seconds = 0`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: false` with an error on field `dogfood_lease.grace_seconds`

#### Scenario: Grace not shorter than the warmer is invalid

- **GIVEN** `.iris.local.toml` declares `[dogfood_lease] warmer_seconds = 30` and no `grace_seconds`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: false` with an error saying `grace_seconds` (30) must be less than `warmer_seconds` (30)

#### Scenario: Block in the shared file warns with a migration hint

- **GIVEN** `.iris.toml` contains a `[dogfood_lease]` block and no `.iris.local.toml`
- **WHEN** `iris:validate_config` is invoked
- **THEN** iris returns `valid: true` with a `local_field_in_shared_config` warning for `dogfood_lease` and the hint `"move dogfood_lease to .iris.local.toml"`
