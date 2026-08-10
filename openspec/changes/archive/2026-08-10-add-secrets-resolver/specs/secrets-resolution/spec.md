## ADDED Requirements

### Requirement: Scheme-prefixed secret source resolution

Iris SHALL resolve a secret source descriptor through a scheme-prefixed dispatch registry. A bare
string with no `://` SHALL be treated as `env://<string>`. Supported schemes are `env`, `keychain`,
and `op`. An unrecognized scheme SHALL be treated as a failed resolve (`ok=false`), never an error
that aborts the caller.

#### Scenario: Bare string resolves against the process environment

- **WHEN** a source descriptor with no `://` is resolved
- **THEN** iris looks it up via `os.LookupEnv` against its own process environment, identically to
  an explicit `env://` prefix

#### Scenario: `env://` resolves against the process environment

- **WHEN** a source descriptor `env://FOO` is resolved
- **THEN** iris looks up `FOO` via `os.LookupEnv` against its own process environment

#### Scenario: `keychain://<service>` resolves via `security find-generic-password`

- **WHEN** a source descriptor `keychain://my-service` is resolved
- **THEN** iris runs `security find-generic-password -s my-service -w` and returns its trimmed
  stdout on a zero exit with non-empty output

#### Scenario: `keychain://<service>/<account>` resolves with an account qualifier

- **WHEN** a source descriptor `keychain://my-service/my-account` is resolved
- **THEN** iris runs `security find-generic-password -s my-service -a my-account -w`

#### Scenario: `op://<vault>/<item>/<field>` resolves via `op read` after bootstrapping

- **GIVEN** `[secrets.op].bootstrap_source` is configured
- **WHEN** a source descriptor `op://vault/item/field` is resolved
- **THEN** iris first resolves `bootstrap_source` through this same registry, then runs `op read
  op://vault/item/field` with the bootstrap credential set under `bootstrap_target` only in that
  subprocess's own environment, and returns the trimmed stdout on a zero exit with non-empty output

#### Scenario: Unresolvable `op://` bootstrap short-circuits without invoking `op`

- **WHEN** `[secrets.op].bootstrap_source` is empty, unset, or fails to resolve
- **THEN** iris does not invoke `op` at all and treats the `op://` source as a failed resolve

#### Scenario: Unrecognized scheme fails to resolve

- **WHEN** a source descriptor uses a scheme other than `env`, `keychain`, or `op` (e.g. `vault://x`)
- **THEN** iris treats it as a failed resolve, not an error

### Requirement: Process-lifetime success-only memoization

Iris SHALL memoize a successful resolve, keyed by the exact source descriptor string, for the
remaining lifetime of the process. A failed resolve SHALL NOT be memoized.

#### Scenario: A successful resolve is cached

- **WHEN** the same source descriptor is resolved twice and the first resolve succeeded
- **THEN** the second resolve returns the cached value without re-invoking any subprocess

#### Scenario: A failed resolve is retried, not cached

- **WHEN** the same source descriptor is resolved twice and the first resolve failed
- **THEN** the second resolve attempts the resolver again (e.g. a transient `op read` network
  failure can succeed on a later attempt)

### Requirement: Fail-open with descriptor-only logging

An unresolved secret source SHALL leave its target environment variable unset in the calling
subprocess's environment and SHALL NOT prevent that subprocess from running. Iris SHALL log a
warning naming only the target variable and the source descriptor — never a resolved value.

#### Scenario: Unresolved source is skipped, not fatal

- **WHEN** a configured `[secrets.env]` source fails to resolve
- **THEN** the target variable is left unset in the subprocess's environment, and the build,
  check, or restart that subprocess belongs to still runs

#### Scenario: A resolved value is never logged

- **WHEN** any resolve (successful or failed) is logged
- **THEN** the log line contains only the target variable name and the source descriptor string,
  never the resolved secret value

### Requirement: Subprocess safety for resolver commands

Before invoking a resolver subprocess (`security`, `op`), iris SHALL confirm the command is
actually resolvable via `exec.LookPath`, not `os.Stat`. Each resolver subprocess SHALL run in its
own process group and SHALL be killed as a whole group on timeout, with a wait-delay backstop.

#### Scenario: A non-executable or directory match is rejected

- **WHEN** the configured resolver command name matches a directory or a non-executable file on
  `PATH`
- **THEN** iris treats the command as unresolvable and fails the resolve, rather than attempting
  to execute it

#### Scenario: A hung resolver subprocess is killed as a whole process group on timeout

- **GIVEN** a resolver subprocess that forks a descendant holding its output pipes open
- **WHEN** the configured timeout elapses
- **THEN** iris kills the entire process group (not just the direct child), bounding the resolve
  to the configured timeout rather than waiting for the descendant to exit on its own

### Requirement: `[secrets]` config schema and validation

Iris SHALL support an optional `[secrets]` block in `.iris.local.toml`, classified as a
`kind:"local"` field. `[secrets.env]` SHALL map target environment variable names to source
descriptor strings. `[secrets.op]` SHALL configure `bootstrap_source` and `bootstrap_target` for
the `op` resolver. An absent `[secrets]` block SHALL be a complete no-op, identical to today's
behavior. A `[secrets.op].bootstrap_source` value that itself uses the `op` scheme SHALL be
rejected as a validation error.

#### Scenario: Absent `[secrets]` block changes nothing

- **WHEN** `.iris.local.toml` has no `[secrets]` block (or the file is absent entirely)
- **THEN** no source is resolved, no target variable is injected anywhere, and build/check/restart
  behavior is unchanged from before this feature existed

#### Scenario: `[secrets]` set in the shared `.iris.toml` warns but is honored

- **WHEN** `[secrets]` is set in the shared `.iris.toml` rather than `.iris.local.toml`
- **THEN** iris honors the value (matching the existing local-field-in-shared-config behavior) but
  surfaces a warning that it belongs in `.iris.local.toml`

#### Scenario: An `op://` bootstrap source is rejected at validation time

- **WHEN** `[secrets.op].bootstrap_source` is set to a value beginning with `op://`
- **THEN** `iris:validate_config` (and any config load that runs validation) reports a structural
  error naming `secrets.op.bootstrap_source`, rather than allowing a configuration that would
  recurse into itself at resolve time

### Requirement: `iris:run_checks` secrets injection

`RunChecks` SHALL load `.iris.local.toml`'s `[secrets]` block for the resolved source repo and
inject every resolvable `[secrets.env]` mapping into `script/iris-check`'s subprocess environment
before it executes.

#### Scenario: Resolved secrets reach the check subprocess

- **GIVEN** `.iris.local.toml` declares `[secrets.env] FOO = "env://FOO_SOURCE"` and
  `FOO_SOURCE` is set in iris's own process environment
- **WHEN** `iris:run_checks` runs `script/iris-check <check>`
- **THEN** the subprocess's environment includes `FOO=<resolved value>`

#### Scenario: An unresolved secret does not block the check

- **GIVEN** `.iris.local.toml` declares a `[secrets.env]` mapping whose source fails to resolve
- **WHEN** `iris:run_checks` runs
- **THEN** the check still runs, with that one target variable left unset and a warning logged
  naming the variable and its source descriptor
