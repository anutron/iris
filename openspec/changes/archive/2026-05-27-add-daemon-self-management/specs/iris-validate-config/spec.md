# iris-validate-config Specification

## ADDED Requirements

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
