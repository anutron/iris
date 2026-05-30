## ADDED Requirements

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
