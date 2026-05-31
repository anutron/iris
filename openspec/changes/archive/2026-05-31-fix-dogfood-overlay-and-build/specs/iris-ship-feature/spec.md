# iris-ship-feature Specification

## ADDED Requirements

### Requirement: CI timeout resolved from the merged overlay

`iris:ship_feature` (`pr-auto` mode) SHALL resolve `ship_ci_timeout_seconds` from the MERGED configuration — `.iris.toml` overlaid with the optional `.iris.local.toml`. `ship_ci_timeout_seconds` is a `local`-tagged field, so a value set only in `.iris.local.toml` SHALL be honored. When neither file sets it, the default (600 seconds) SHALL apply. A missing or unparseable configuration SHALL fall back to the default rather than failing the ship.

#### Scenario: Timeout read from .iris.local.toml

- **GIVEN** a source repo whose `.iris.toml` does NOT set `ship_ci_timeout_seconds` but whose gitignored `.iris.local.toml` sets `ship_ci_timeout_seconds = 900`
- **WHEN** `iris:ship_feature` resolves the CI-wait timeout for a `pr-auto` ship
- **THEN** it uses 900 seconds, not the 600-second default

#### Scenario: Default applies when unset in both files

- **GIVEN** a source repo whose merged config does not set `ship_ci_timeout_seconds`
- **WHEN** `iris:ship_feature` resolves the CI-wait timeout
- **THEN** it uses the 600-second default

#### Scenario: Missing or unreadable config falls back to the default

- **GIVEN** a source repo with no readable `.iris.toml`
- **WHEN** `iris:ship_feature` resolves the CI-wait timeout
- **THEN** it uses the 600-second default and does not fail the ship on the missing config
