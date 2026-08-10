**Design doc:** `openspec/changes/add-secrets-resolver/design.md`

## 1. Tests

- [ ] 1.1 Config schema tests: `SecretsBlock`/`OpSecretConfig` TOML decode (`[secrets.env]` +
      `[secrets.op]`), `kind:"local"` taxonomy classification, `bootstrap_source` starting with
      `op://` rejected as a validation error, absent `[secrets]` is a no-op, `[secrets]` set in the
      shared `.iris.toml` warns but is honored
- [ ] 1.2 `internal/secrets` package tests: bare-string/`env://` resolve, `keychain://<service>`
      and `keychain://<service>/<account>` resolve (via injectable subprocess seam, never a real
      `security` call), `op://<vault>/<item>/<field>` resolve including the bootstrap-then-read
      flow (via the same injectable seam, never a real `op` call), unrecognized scheme fails
      cleanly, success-only memoization (failed resolve retried, successful resolve cached),
      `exec.LookPath`-based resolvability check (rejects a directory/non-executable match),
      process-group timeout kill (a fake subprocess that forks a descendant holding stdout open
      is still bounded by the timeout), `ResolveEnv` never logs a resolved value
- [ ] 1.3 `RunBuild` wiring tests: resolved secrets reach the subprocess via both
      `script/iris-build` and the Makefile fallback; absent config is a no-op; an unresolved
      secret leaves its target unset and does not block the build
- [ ] 1.4 `RunChecks` wiring tests: same shape as 1.3 for `script/iris-check`
- [ ] 1.5 `reload.go` wiring tests: `runBuildBlock` injects resolved secrets alongside existing
      `[build].env`; `dispatchRestart`'s `exec` mechanism injects resolved secrets; other restart
      mechanisms are unaffected; a malformed `.iris.local.toml` logs a warning and resolves zero
      secrets rather than aborting the reload/publish
- [ ] 1.6 `publish.go` test confirming a `[secrets.env]` mapping reaches both the build and
      (`exec` mechanism) restart subprocess environments during `iris:publish`, matching reload

## 2. Config schema

**Depends on:** Stage 1

- [ ] 2.1 Add `SecretsBlock` (`Env map[string]string \`toml:"env"\``, `Op OpSecretConfig
      \`toml:"op"\``) and `OpSecretConfig` (`BootstrapSource`, `BootstrapTarget`) to
      `internal/config/iris_toml.go`
- [ ] 2.2 Add `Secrets SecretsBlock` to `IrisToml` with `toml:"secrets" json:"secrets,omitempty"
      kind:"local"`
- [ ] 2.3 Add `(*SecretsBlock).validate()` (or equivalent) wired into `IrisToml.Validate`:
      reject a `bootstrap_source` beginning with `op://`; treat empty target/source keys as
      structural errors, matching the `ValidationError{Field, Message, Hint}` shape used
      elsewhere in this file
- [ ] 2.4 Run `iris_toml_taxonomy_test.go`'s exhaustiveness test; confirm the new field is picked
      up automatically via reflection (fix the test only if it hardcodes a field list rather than
      deriving it)
- [ ] 2.5 `go test ./internal/config/...` green

## 3. Resolver package (`internal/secrets`)

**Depends on:** Stage 2

- [ ] 3.1 Scaffold `internal/secrets` package: `splitSecretScheme` (defaults to `env` when no
      `://`) and scheme dispatch
- [ ] 3.2 `env` scheme resolver (`os.LookupEnv`)
- [ ] 3.3 `keychain` scheme resolver (`security find-generic-password -s <service> [-a
      <account>] -w`) via an injectable subprocess-runner seam (var-swap function field,
      matching this codebase's existing test-seam conventions — never a real `security` call in
      tests)
- [ ] 3.4 `op` scheme resolver: resolves `[secrets.op].bootstrap_source` through the *same*
      `Resolve` function first, then runs `op read op://<vault>/<item>/<field>` with the
      bootstrap credential set under `bootstrap_target` only in that subprocess's own env (via
      the same injectable seam)
- [ ] 3.5 Process-lifetime success-only memoization (package-level map + mutex, keyed by exact
      descriptor string)
- [ ] 3.6 Subprocess safety for the resolver's own commands: `exec.LookPath` (not `os.Stat`) for
      resolvability, `Setpgid` + a `Cancel` that `syscall.Kill`s the process group + `WaitDelay`
      backstop on timeout
- [ ] 3.7 `ResolveEnv(ctx context.Context, sc config.SecretsBlock) []string`: resolves every
      `[secrets.env]` mapping, returns `"TARGET=value"` entries for successes, `slog.Warn`s
      (naming only the variable and descriptor, never the value) for failures
- [ ] 3.8 `go test ./internal/secrets/...` green

## 4. Wire into `RunBuild`

**Depends on:** Stage 3

- [ ] 4.1 In `run_build.go`, load `config.LoadOverlay(resolved.SourceRepo, false)` before
      constructing `cmd`; on an I/O error or nil `Doc`, log via `slog.Warn` and proceed with no
      secrets (fail-open, matching `merge_to_branch.go`'s existing "skip, warn, continue"
      precedent) — never fail the build over a config-load problem
- [ ] 4.2 Call `secrets.ResolveEnv(ctx, doc.Secrets)` and append its output to `cmd.Env` (seeded
      from `os.Environ()` first, since `cmd.Env` is nil today) for both the `script/iris-build`
      and Makefile-fallback branches
- [ ] 4.3 `go test ./internal/verbs/... -run RunBuild` green (tests from 1.3)

## 5. Wire into `RunChecks`

**Depends on:** Stage 3

- [ ] 5.1 Same shape as 4.1/4.2 in `run_checks.go`
- [ ] 5.2 `go test ./internal/verbs/... -run RunChecks` green (tests from 1.4)

## 6. Wire into `reload.go` (covers `iris:reload` and `iris:publish`)

**Depends on:** Stage 3

- [ ] 6.1 Add a small helper (in `internal/config`, mirroring `PeekLocalDogfoodBranch`'s
      lenient-read style) that reads `.iris.local.toml`'s `[secrets]` block only, returning the
      zero value on any problem but logging via `slog.Warn` on a parse failure (unlike
      `PeekLocalDogfoodBranch`, which stays silent — a silently-empty `[secrets]` block is exactly
      the failure this change must make visible)
- [ ] 6.2 Thread the resolved `SecretsBlock` into `runBuildBlock` and append
      `secrets.ResolveEnv`'s output onto its existing `mergedEnv(b.Env)` result
- [ ] 6.3 Thread the resolved `SecretsBlock` into `dispatchRestart`'s `MechanismExec` case only;
      extend `runArgv` (or add a variant) to accept extra env entries and append them onto
      `os.Environ()` for that subprocess
- [ ] 6.4 Update both `Reload` and `Publish` call sites to fetch the local secrets once (via 6.1)
      and pass them into `runBuildBlock`/`dispatchRestart`
- [ ] 6.5 `go test ./internal/verbs/... -run 'Reload|Publish'` green (tests from 1.5 and 1.6)

## 7. Verify

**Depends on:** Stages 4, 5, 6

- [ ] 7.1 `openspec validate add-secrets-resolver --strict`
- [ ] 7.2 `make build && make test && make vet` all green
- [ ] 7.3 Sanity-check `iris_validate_config` against a sample `.iris.local.toml` containing an
      `op://`-prefixed `bootstrap_source`, confirming the structural rejection from task 2.3
      surfaces correctly
- [ ] 7.4 Check for signs of concurrent agent activity in the Sketch project (recent commits,
      running dev server/DB) before touching shared state; if clear, configure
      `thanx/sketch/.iris.local.toml` with the placeholder `op://claude/shell-env/
      BUNDLE_PACKAGES__THANX__COM` descriptor and run `iris_run_build` end-to-end, confirming
      `bundle install` now succeeds against `packages.thanx.com`
- [ ] 7.5 Report results back to Aaron, including whether end-to-end validation ran (or why it
      was skipped) and that `thanx/sketch/script/iris-credentials.sh`'s Keychain/`op`-reading
      logic is now safe to remove in that repo's own PR (not done here)

## 8. Archive

**Depends on:** Stage 7

- [ ] 8.1 `openspec archive add-secrets-resolver`
