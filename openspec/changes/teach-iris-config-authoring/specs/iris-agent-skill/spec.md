# iris-agent-skill Specification

## ADDED Requirements

### Requirement: Config-authoring guidance

The `iris` skill SHALL teach an agent how to author both iris config files: `.iris.toml` (the checked-in, project-wide file) and `.iris.local.toml` (the gitignored, per-developer overlay). The guidance MAY live in a dedicated reference file within the skill directory (loaded on demand) provided `claude/skills/iris/SKILL.md` points the agent to it. The guidance SHALL cover: the two files and their overlay relationship; the field taxonomy classifying each field as `shared` (belongs in `.iris.toml`) or `local` (belongs in `.iris.local.toml`) and what happens when a field is placed in the wrong file; the `[build]` and `[restart]` blocks; every supported `[restart] mechanism` value; the rule that `exit_code` is legal only for iris's own repo while every other managed daemon must use a different mechanism; the no-side-effect `iris_validate_config` authoring loop and the `iris_set_local_config` writer for local fields; and at least one worked example config.

#### Scenario: SKILL.md points to the config-authoring guidance

- **WHEN** `claude/skills/iris/SKILL.md` is read
- **THEN** it names both `.iris.toml` and `.iris.local.toml`, gives a short orientation to the shared-vs-local split, and directs the agent to the full config-authoring reference (a dedicated file in the skill directory) for the complete schema

#### Scenario: Both files and the overlay relationship are documented

- **WHEN** the config-authoring guidance is read
- **THEN** it states that `.iris.toml` is checked in and project-wide, that `.iris.local.toml` is gitignored and per-developer, and that the local file is an overlay applied on top of the shared file rather than a standalone fallback

#### Scenario: Field taxonomy is documented

- **WHEN** the config-authoring guidance is read
- **THEN** it classifies each field as `shared` or `local`, placing at least `schema_version`, `default_branch`, `[build]`, and `[restart]` in `.iris.toml` and at least `dogfood_branch` and `ship_ci_timeout_seconds` in `.iris.local.toml`, and explains that a misplaced field warns (`local_field_in_shared_config` / `shared_field_in_local_config`) rather than silently doing the wrong thing

#### Scenario: Restart mechanisms and the exit_code self-only rule are documented

- **WHEN** the config-authoring guidance is read
- **THEN** it lists the supported `[restart] mechanism` values (`exit_code`, `launchagent`, `launchdaemon`, `signal`, `exec`, `none`) with the fields each requires, and states that `exit_code` is legal only for iris's own repo while any other managed daemon must use a different mechanism

#### Scenario: The validate / set authoring loop is documented

- **WHEN** the config-authoring guidance is read
- **THEN** it directs the agent to validate edits with `iris_validate_config` (noting it has no side effects) and to write `.iris.local.toml` through `iris_set_local_config` rather than by hand, and includes at least one complete worked `.iris.toml` example
