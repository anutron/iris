## ADDED Requirements

### Requirement: Secrets resolution for the build step

`runBuildBlock` SHALL read the source repo's `.iris.local.toml` `[secrets]` block and inject every
resolvable `[secrets.env]` mapping into the `[build] command` subprocess's environment, alongside
(and without disturbing) the existing `[build].env` static mapping. An absent or empty `[secrets]`
block SHALL leave the build's environment exactly as it is today.

#### Scenario: Resolved secrets reach the reload build step

- **GIVEN** the source repo's `.iris.local.toml` declares a resolvable `[secrets.env]` mapping
- **WHEN** `iris:reload` runs its build step
- **THEN** the `[build] command` subprocess's environment includes the resolved mapping alongside
  any `[build].env` static entries

#### Scenario: An unresolved secret does not abort the reload

- **GIVEN** a `[secrets.env]` mapping whose source fails to resolve
- **WHEN** `iris:reload` runs its build step
- **THEN** the build still runs (subject to its own existing success/failure semantics), with that
  target variable left unset and a warning logged naming the variable and its source descriptor

### Requirement: Secrets resolution for the exec restart mechanism

When `[restart] mechanism = "exec"`, `dispatchRestart` SHALL read the source repo's
`.iris.local.toml` `[secrets]` block and inject every resolvable `[secrets.env]` mapping into the
restart command's subprocess environment. Other restart mechanisms (`exit_code`, `launchagent`,
`launchdaemon`, `signal`, `none`) do not exec a project-configured command with a customizable
environment and are unaffected.

#### Scenario: Resolved secrets reach the exec restart command

- **GIVEN** `[restart] mechanism = "exec"` and the source repo's `.iris.local.toml` declares a
  resolvable `[secrets.env]` mapping
- **WHEN** `iris:reload` dispatches the restart
- **THEN** the restart command's subprocess environment includes the resolved mapping

#### Scenario: Non-exec mechanisms are unaffected

- **WHEN** `[restart] mechanism` is `exit_code`, `launchagent`, `launchdaemon`, `signal`, or `none`
- **THEN** no secrets resolution or injection occurs for the restart step (unchanged from before
  this feature existed)
