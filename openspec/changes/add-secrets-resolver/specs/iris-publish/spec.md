## ADDED Requirements

### Requirement: Secrets resolution matches reload

`iris:publish` SHALL inject resolved `[secrets.env]` mappings from the source repo's
`.iris.local.toml` into its build step and (when `[restart] mechanism = "exec"`) its restart step,
identically to `iris:reload` — because it invokes the same `runBuildBlock` and `dispatchRestart`
implementations.

#### Scenario: Resolved secrets reach a publish's build and exec-restart steps

- **GIVEN** the source repo's `.iris.local.toml` declares a resolvable `[secrets.env]` mapping and
  `[restart] mechanism = "exec"`
- **WHEN** `iris:publish` runs its build and restart steps
- **THEN** both subprocess environments include the resolved mapping, identically to `iris:reload`

#### Scenario: An unresolved secret does not abort the publish

- **GIVEN** a `[secrets.env]` mapping whose source fails to resolve
- **WHEN** `iris:publish` runs
- **THEN** the publish proceeds (subject to its own existing success/failure semantics), with that
  target variable left unset and a warning logged naming the variable and its source descriptor
