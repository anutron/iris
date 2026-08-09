## ADDED Requirements

### Requirement: Secrets resolution before build subprocess exec

`RunBuild` SHALL load `.iris.local.toml`'s `[secrets]` block for the resolved source repo (via
`config.LoadOverlay`) and inject every resolvable `[secrets.env]` mapping into the build
subprocess's own environment before it executes, whether the build runs via `script/iris-build` or
the Makefile fallback. A missing `.iris.toml`/`.iris.local.toml`, or a `[secrets]` block that is
absent or empty, SHALL leave the build's environment unchanged from today.

#### Scenario: Resolved secrets reach the build subprocess via `script/iris-build`

- **GIVEN** `.iris.local.toml` declares `[secrets.env] FOO = "env://FOO_SOURCE"` and
  `FOO_SOURCE` is set in iris's own process environment
- **WHEN** `iris:run_build` runs `script/iris-build`
- **THEN** the subprocess's environment includes `FOO=<resolved value>`

#### Scenario: Resolved secrets reach the build subprocess via the Makefile fallback

- **GIVEN** the worktree has no `script/iris-build` but has a `Makefile`, and `.iris.local.toml`
  declares a resolvable `[secrets.env]` mapping
- **WHEN** `iris:run_build` runs `make build`
- **THEN** the `make` subprocess's environment includes the resolved mapping

#### Scenario: No config present changes nothing

- **WHEN** the resolved source repo has no `.iris.toml`/`.iris.local.toml` at all
- **THEN** `iris:run_build` runs exactly as it did before this feature existed — no secrets
  resolution is attempted, and the build is not blocked by the absence of config

#### Scenario: An unresolved secret does not block the build

- **GIVEN** `.iris.local.toml` declares a `[secrets.env]` mapping whose source fails to resolve
- **WHEN** `iris:run_build` runs
- **THEN** the build still runs, with that one target variable left unset and a warning logged
  naming the variable and its source descriptor
