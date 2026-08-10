## Why

`script/iris-credentials.sh` in thanx/sketch self-provisions `BUNDLE_PACKAGES__THANX__COM` by
reading `OP_SERVICE_ACCOUNT_TOKEN` from its own process environment and shelling out to `op read`
— but that token only reaches it via fragile ambient inheritance from however iris happened to be
started, the same class of assumption that caused a real incident in argus (see design.md
Context). Aaron does not want the target repo aware of Keychain/1Password at all. Iris needs its
own config-driven secrets resolver, ported from the pattern argus just built and validated, so
credentials are resolved fresh inside iris itself and injected only into the specific subprocess
that needs them.

## What Changes

- New `internal/secrets` package: a scheme-prefixed (`env://`, `keychain://`, `op://`) secret
  source resolver with a small dispatch registry, process-lifetime success-only memoization, and
  the two subprocess-safety fixes argus's review already found (`exec.LookPath` over `os.Stat`;
  process-group kill on timeout via `Setpgid`+`Cancel`+`WaitDelay`).
- New `[secrets]` block in `.iris.local.toml` (gitignored, per-developer — not the shared
  `.iris.toml`): `[secrets.env]` maps target env-var names to source descriptors;
  `[secrets.op]` configures the `op` resolver's own bootstrap (`bootstrap_source`,
  `bootstrap_target`), itself resolved through the same registry.
- Wiring into all four exec call sites that run a project's configured build/check/restart
  command: `RunBuild` (`run_build.go`), `RunChecks` (`run_checks.go`), `runBuildBlock` and
  `dispatchRestart`'s `exec` mechanism (`reload.go`, shared by both `iris:reload` and
  `iris:publish`). Each resolves `[secrets.env]` fresh via one shared `secrets.ResolveEnv` helper
  and appends the result to that subprocess's own `cmd.Env` — never iris's own ambient
  environment.
- Fail-open: an unresolved source leaves its target var unset and logs (`slog.Warn`) only the
  variable name and source descriptor, never a resolved value. Never blocks the build/check/
  restart from running.
- `RunBuild`/`RunChecks` gain their first `.iris.toml`/`.iris.local.toml`-loading step
  (`config.LoadOverlay`) — previously they discovered `script/iris-build`/`script/iris-check`
  purely by filesystem convention with no config consulted at all. This change adds config-loading
  only far enough to read `[secrets]`; it does not also wire in the pre-existing, unrelated gap
  where `[build].env`/`timeout_seconds`/`working_directory` go unused by these two verbs.

## Capabilities

### New Capabilities

- `secrets-resolution`: the scheme-prefixed secret resolver registry itself (dispatch, config
  schema, memoization, fail-open behavior, subprocess safety) and its integration into `RunChecks`
  (which has no existing base spec to attach a delta to — see Discovery findings in design.md).

### Modified Capabilities

- `iris-run-build`: `RunBuild` now loads `.iris.local.toml`'s `[secrets]` block and injects
  resolved values into the build subprocess's environment before it executes.
- `iris-reload`: the build step (`runBuildBlock`) and the `exec` restart mechanism
  (`dispatchRestart`) now inject resolved `[secrets.env]` values into their respective subprocess
  environments.
- `iris-publish`: publish shares `runBuildBlock`/`dispatchRestart` with reload, so its build and
  `exec`-mechanism restart steps gain the same secrets injection.

## Impact

- **New code:** `internal/secrets/` (resolver package + tests).
- **Modified code:** `internal/config/iris_toml.go` (new `SecretsBlock`/`OpSecretConfig` types,
  `kind:"local"` taxonomy tag, validation), `internal/verbs/run_build.go`, `internal/verbs/
  run_checks.go`, `internal/verbs/reload.go` (`runBuildBlock`, `dispatchRestart`, plus a small new
  helper to read `.iris.local.toml`'s `[secrets]` block for `Reload`/`Publish`, which previously
  loaded only the shared file).
- **No changes** to `internal/verbs/publish.go` itself — it calls the same `runBuildBlock`/
  `dispatchRestart` functions being fixed in `reload.go`.
- **Downstream (separate repo, separate PR, not part of this change):** once this ships and Aaron
  configures `thanx/sketch/.iris.local.toml`, the Keychain/`op`-reading logic in
  `thanx/sketch/script/iris-credentials.sh` becomes unnecessary and can be removed in that repo's
  own review process.
- **New dependency:** none — `security` and `op` are invoked as external CLIs already assumed
  present on Aaron's machine (same assumption argus's own resolver makes), not linked as Go
  libraries.
