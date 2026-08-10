## Context

Iris manages `script/iris-build`, `script/iris-check`, and the `[restart]` `exec` mechanism
(`iris-restart`) for projects it manages (e.g. thanx/sketch). Some of these subprocesses need a
credential that must not be hardcoded or committed — today, specifically
`BUNDLE_PACKAGES__THANX__COM`, a Socket Firewall / private-gem-source credential that
`bundle install` needs against `packages.thanx.com`.

The current fix lives entirely in the *target* repo: `thanx/sketch/script/iris-credentials.sh` is
sourced by `script/iris-build`, `script/iris-check`, and `script/iris-restart`. It self-provisions
`BUNDLE_PACKAGES__THANX__COM` by reading `OP_SERVICE_ACCOUNT_TOKEN` from its own process
environment and shelling out to `op read`. Its own header comment explains why this is fragile:
`OP_SERVICE_ACCOUNT_TOKEN` reaches that process only because whatever launched iris happened to
have it in its environment (today, ambient inheritance from however iris itself was started) — an
assumption identical to the one that caused a real incident in argus (see below), and one Aaron
explicitly does not want living in the target repo's scripts at all. Sketch should not need to
know iris uses Keychain or 1Password, or how Aaron organizes his personal credentials.

**This is a deliberate port of a pattern argus just built and validated** (branch
`argus/credential-bootstrap`, `openspec/changes/archive/2026-08-09-add-secrets-resolver-registry/`
in the argus repo). Argus hit the same root problem — a wrapper script fetching a credential and
exporting it into whatever process happened to fork the daemon, which silently broke every time a
*different* code path did the forking (TUI auto-reconnect, supervisor self-restart, `launchctl
kickstart` all being independent processes with independent environments). Argus's fix: resolve a
secret fresh, at the point of use, inside whichever process actually execs the subprocess that
needs it — never assume a value has propagated by env inheritance from some other process.

Iris has an almost exact structural echo of argus's "which process actually forks" problem, but at
one level of remove: instead of "which process forks the daemon", it's "which code path execs the
target repo's build/check/restart command." Discovery below found this is not one call site per
concern (build/check/restart) — it's four, because `reload.go`'s `runBuildBlock` and
`dispatchRestart` are shared by both `Reload` and `Publish`.

## Goals / Non-Goals

**Goals:**

- A secret source descriptor is self-describing (URI-scheme prefixed: `env://`, `keychain://`,
  `op://`) and resolves through a small, config-driven resolver registry — never hardcoded to a
  specific vault/item/Keychain-service name in Go code.
- The target repo (Sketch, or any other project iris manages) needs zero Keychain/1Password
  awareness. `script/iris-credentials.sh`'s Keychain/`op`-reading logic becomes unnecessary once
  this ships (removal is a separate PR in the target repo, out of scope here — see Migration Plan).
- A secret resolves fresh, at the point of use, inside whichever iris code path actually execs the
  subprocess that needs it — never assumed to propagate via process-environment inheritance.
- Every exec call site that runs a project's configured build/check/restart command gets secrets
  injected identically: `RunBuild`, `RunChecks`, `runBuildBlock` (reload+publish), and
  `dispatchRestart`'s `exec` mechanism (reload+publish). Missing any one of these reproduces
  exactly the "silently broke through an untested code path" failure mode argus's incident writeup
  describes.
- Fail-open: an unresolved source leaves the target var unset and logs only the
  variable/descriptor name — never a value — and never blocks the build/check/restart from
  running. A misconfigured or unreachable secret source must not make iris itself unusable.
- Everything is config-driven per project via `.iris.local.toml`. No 1Password vault/item name or
  Keychain service name is ever hardcoded in Go.

**Non-Goals:**

- No `argus doctor`/Settings-style dedicated status surface for secrets resolution in this change.
  Fail-open + `slog.Warn` (naming only the variable/descriptor, matching this codebase's existing
  `merge_to_branch.go` "skip hook, warn, continue" precedent) is the only visibility mechanism for
  v1. Extendable later via `iris_validate_config`/`iris_status` if wanted.
- Not retroactively fixing the pre-existing gap where `RunBuild`/`RunChecks` don't honor
  `[build].env`/`timeout_seconds`/`working_directory` today (they only discover the script by
  filesystem convention). This change teaches them to load config for the first time, but only to
  read `[secrets]` — the other unused `[build]` fields stay unused, unchanged from today.
- No credential rotation, expiry, or refresh policy. No multi-user/remote secret store — single
  developer, single machine, matching iris's existing single-operator scope.
- Not building a general plugin system for arbitrary resolver schemes. Three built-ins (`env`,
  `keychain`, `op`) cover the known need; a fourth scheme is a small additive change later.
- Not deleting `thanx/sketch/script/iris-credentials.sh` as part of this change — that's a
  separate PR in a separate repo with its own review process (see Migration Plan).

## Decisions

### Decision: `[secrets]` lives in `.iris.local.toml`, not `.iris.toml`

Confirmed with Aaron. This codebase already has a `kind:"shared"`/`kind:"local"` taxonomy
(`internal/config/iris_toml_taxonomy.go`) drawing exactly this line: `default_branch` is
project-wide and shared; `dogfood_branch` is per-developer and local, precisely because it names
something specific to one person's setup. A secret source descriptor (which Keychain service,
which 1Password vault/item) is the same kind of fact — it's Aaron's personal credential-store
layout, not a project-wide fact that would be identical for a second developer working on the same
repo. Keeping it in `.iris.local.toml` (gitignored) also means a vault/item/service name never
lands in the target repo's shared git history, even though the descriptor itself isn't a secret
value.

Cost: `reload.go`'s `Reload` and `publish.go`'s `Publish` currently load only the shared file
(`config.LoadIrisTomlMode`) — `.iris.local.toml` isn't consulted there at all today (only
`run_build.go`/`run_checks.go` are new to config-loading; those will use `config.LoadOverlay`,
which already merges both files). A new small helper reads just the local file's `[secrets]` block
for `Reload`/`Publish` to pass into `runBuildBlock`/`dispatchRestart`, in the same
lenient/never-blocks style as the existing `PeekLocalDogfoodBranch` (returns the zero value on any
problem — missing file, malformed TOML — but logs via `slog.Warn` on a parse failure, since a
silently-empty `[secrets]` block is exactly the kind of failure this change is supposed to make
visible, unlike `PeekLocalDogfoodBranch`'s silent branch-name peek).

**Alternative considered:** shared `.iris.toml`. Simpler — `Reload`/`Publish` already load that
file, so zero new plumbing there. Rejected because it fights the taxonomy this codebase just
built for exactly this reason.

### Decision: `[secrets.env]` + `[secrets.op]`, mirroring the existing `[build.env]` convention

Confirmed with Aaron. `[secrets]` needs both a target-var → source-descriptor mapping and an
`op`-bootstrap sub-config. BurntSushi TOML can't decode a bare `map[string]string` and a nested
struct field cleanly in the same table, but this codebase already has the answer:
`BuildBlock.Env map[string]string` decodes from a `[build.env]` sub-table, sibling to `Command`/
`TimeoutSeconds`/`WorkingDirectory` on the same parent struct. `[secrets]` does the same thing —
`Env` (the mapping) and `Op` (the bootstrap sub-config) are sibling fields on one `SecretsBlock`
struct, each with its own nested table:

```toml
# .iris.local.toml
[secrets.env]
BUNDLE_PACKAGES__THANX__COM = "op://claude/shell-env/BUNDLE_PACKAGES__THANX__COM"

[secrets.op]
bootstrap_source = "keychain://op-service-account-claude"
bootstrap_target = "OP_SERVICE_ACCOUNT_TOKEN"
```

No new TOML idiom is introduced — this is the same shape `[build.env]` already establishes.

`op://claude/shell-env/BUNDLE_PACKAGES__THANX__COM` above is Aaron's current placeholder,
matching what's live in his 1Password today. Nothing in Go code assumes this vault/item name;
Aaron updates `.iris.local.toml` directly once his vault restructuring (splitting `shell-env` from
a narrower `agent-env` item) is done — no code change required either way.

### Decision: URI-scheme-prefixed descriptors, dispatched through a small resolver registry

Ported from argus's design (`internal/agent/secretregistry.go`): a bare string or `env://<name>`
resolves against the process environment (`os.LookupEnv`); `keychain://<service>` or
`keychain://<service>/<account>` shells out to `security find-generic-password`; `op://<vault>/
<item>/<field>` shells out to `op read` after first resolving its own bootstrap credential via
`[secrets.op]`. This mirrors the established pattern for this problem space (Vault secret engines,
external-secrets-operator `SecretStore` providers, sops, direnv) — the descriptor names its own
resolver, so config only supplies resolver-specific parameters, never a global "the" resolver.

### Decision: resolve at point of use — no ambient injection, ever

The resolver is called fresh inside each of the four wiring sites, injecting the resolved value
only into that one subprocess's own `cmd.Env` (`append`-style, alongside `os.Environ()` or the
call site's existing `mergedEnv`). Nothing calls `os.Setenv` on iris's own process environment.
This is the direct structural analog of argus's core fix and the reason the target repo's own
`iris-credentials.sh` self-provisioning script is unnecessary once this lands: today that script
re-fetches the credential inside *every* separate script invocation (`iris-build`,`iris-check`,
`iris-restart`) precisely because there's no shared long-lived process to inherit from — this
change moves that same "re-resolve per subprocess" responsibility into iris itself, generically,
instead of duplicating shell logic per target repo.

### Decision: `op` bootstrap resolves through the same registry `Resolve` function — no special case

`[secrets.op].bootstrap_source` can be any scheme (typically `keychain://...`, but nothing stops
it from being `env://` for a shell that already has `OP_SERVICE_ACCOUNT_TOKEN` ambiently
available). The `op` scheme resolver calls the same `Resolve` function to fetch its own bootstrap
credential, so there's no separate "how does op authenticate" code path — a future fourth scheme
becomes a bootstrap option automatically.

**New guard not present in argus's version:** config validation rejects `bootstrap_source` values
that are themselves `op://...` — resolving `op`'s own bootstrap via `op` is either a no-op typo or
infinite self-reference (the same `SecretsBlock` gets passed to the recursive `Resolve` call, so
an `op://` bootstrap source would recurse into `opSchemeResolve` again against identical
arguments and never terminate). Argus's version has no such guard; iris adds it because a
config-validation-time check is cheap and turns a stack-overflow footgun into a clear
`iris_validate_config` error instead.

### Decision: one shared `ResolveEnv` helper, not four copies of the same loop

Argus has exactly one call site (`BuildCmd`) that walks an `EnvVars` map and resolves each entry.
Iris has four. Rather than port the loop-and-log logic into each of `RunBuild`, `RunChecks`,
`runBuildBlock`, and `dispatchRestart` independently, the new `internal/secrets` package exports:

```go
func ResolveEnv(ctx context.Context, sc config.SecretsBlock) []string
```

— returning ready-to-append `"TARGET=value"` entries for every resolvable `[secrets.env]` mapping,
and logging (`slog.Warn`, naming only the variable and its source descriptor, never the value)
for every one that fails to resolve. All four wiring sites call this one function and `append` its
result onto their own `cmd.Env`. This keeps the fail-open + never-log-a-value invariant in exactly
one place instead of four.

Threading `ctx` through (unlike argus, which hardcodes `context.Background()` for the resolver
subprocess) means a cancelled build/check/restart request also cancels an in-flight resolver
subprocess, rather than leaving it to run out its own independent timeout after the caller has
already given up.

### Decision: reuse argus's two empirically-proven subprocess-safety fixes

1. `exec.LookPath` (not `os.Stat`) to check a configured command (`security`, `op`) is actually
   resolvable — `os.Stat` wrongly accepts a directory or a non-executable file as a match.
2. Process-group kill on timeout: `cmd.SysProcAttr.Setpgid = true` + a `cmd.Cancel` that
   `syscall.Kill`s the whole process group + a `cmd.WaitDelay` backstop, rather than relying on
   `exec.CommandContext`'s default `Cancel`, which only signals the direct child and silently
   fails to bound `cmd.Wait()` if that child forks a descendant holding the output pipes open.
   (Proven empirically in argus's own PR #928 review: a 300ms configured timeout waited the full
   30s without this fix.)

Note this is specifically for the *new* resolver subprocess calls (`security`, `op` invocations)
this change introduces. `reload.go`'s existing `runArgv`/`runBuildBlock`/`runHook` already have
their own (differently-shaped but equivalent) `Setpgid` + `ctx.Done()`-triggered
`syscall.Kill(-pid, ...)` protection for the *target* build/restart commands themselves — that
part needs no change.

## Risks / Trade-offs

- **[Risk]** A resolver shells out to `security`/`op` synchronously on whatever goroutine first
  needs a secret. → **[Mitigation]** Success-only, process-lifetime memoization (keyed by exact
  descriptor string) means this cost is paid once per iris process lifetime per source, not once
  per build/check/restart. A failed resolve is never memoized, so a transient failure (network
  blip on `op read`) can succeed on the next attempt.
- **[Risk]** `.iris.local.toml` failing to parse (unrelated syntax error elsewhere in the file)
  could silently zero out `[secrets]` for a build that needs it. → **[Mitigation]** Logged via
  `slog.Warn` at the point secrets are skipped (matching the `merge_to_branch.go` "skipped due to
  load failure" precedent) — visible in iris's own logs even without a dedicated status command.
- **[Risk]** Four call sites is more surface area than argus's one, raising the chance of drift
  (e.g. a fifth call site added later that forgets to call `ResolveEnv`). → **[Mitigation]** The
  shared `ResolveEnv` helper means the actual resolve-and-inject logic is factored once; a new call
  site only needs to remember to call it, not reimplement it. Tasks below add a test asserting
  each of the four call sites appends `ResolveEnv`'s output.
- **[Risk]** A resolver source descriptor is logged in error paths for debugging. →
  **[Mitigation]** Only the descriptor (e.g. `op://claude/shell-env/FOO`), never a resolved value,
  is ever logged — enforced by `ResolveEnv`/`Resolve` never returning the value to a log call, only
  to `cmd.Env`.

## Migration Plan

1. Land the resolver package + config schema + wiring into all four call sites (this change).
2. Aaron adds `[secrets.env]`/`[secrets.op]` to `thanx/sketch/.iris.local.toml` (gitignored,
   per-developer — not part of this change, since it lives outside the iris repo).
3. Once confirmed working end-to-end against Sketch, Aaron separately reviews/removes
   `thanx/sketch/script/iris-credentials.sh`'s Keychain/`op`-reading logic in that repo's own PR
   process (explicitly out of scope for this change to touch — different repo).
4. Rollback: an absent/empty `[secrets]` block is a strict no-op (no `[secrets.env]` entries to
   resolve, `[secrets.op]` unconfigured) — full backward compatibility, not a hard cutover. If the
   resolver misbehaves, deleting the `[secrets]` block from `.iris.local.toml` reverts to today's
   behavior (whatever ambient environment iris happens to have, unchanged).

## Open Questions

None outstanding — file location (local vs shared) and TOML shape (`[secrets.env]`+`[secrets.op]`)
were each confirmed with Aaron during brainstorming.

## Alternatives considered

Captured inline under each Decision above.

## Discovery findings

- `run_build.go`'s `RunBuild` and `run_checks.go`'s `RunChecks` load **no** `.iris.toml` config
  today — they discover `script/iris-build`/`script/iris-check` purely by filesystem convention
  (existence + executable bit). Wiring in secrets means teaching them to load config
  (`config.LoadOverlay`) for the first time. Deliberately not also wiring in `[build].env`/
  `timeout_seconds`/`working_directory` while doing so — that's a separate, pre-existing gap.
- `reload.go`'s `runBuildBlock` and `dispatchRestart` are **not** private to `Reload` —
  `publish.go`'s `Publish` calls the exact same two functions. Fixing them once in `reload.go`
  covers both `iris_reload` and `iris_publish`'s build/restart steps. This mirrors argus's own key
  finding almost exactly ("`BuildCmd`'s only caller is `runner.go`'s `Start` ... in supervisor mode
  this executes inside the session-supervisor process, not the daemon") — the lesson in both cases
  is that the code path that actually execs the subprocess isn't the one call site a naive design
  would assume.
- `reload.go`'s existing `runArgv`/`runBuildBlock`/`runHook` already have process-group-kill
  timeout protection for the target build/restart commands (via `ctx.Done()` +
  `syscall.Kill(-pid, ...)`) — this pre-dates this change and needs no modification. Only the
  *new* resolver-internal subprocess calls (`security`, `op`) need the argus-style
  `Setpgid`+`Cancel`+`WaitDelay` fix, since those are new call sites this change introduces.
- `BuildBlock.Env map[string]string` (decoding `[build.env]`) is direct, existing precedent in
  this exact codebase for "a map field nested under its own sub-table, sibling to scalar fields on
  the same parent struct" — this is what makes `[secrets.env]` + `[secrets.op]` a natural fit
  rather than a new TOML idiom.
- `internal/config/iris_toml_taxonomy.go`'s `kind` tag classification is computed via reflection
  over `IrisToml`'s fields with a companion exhaustiveness test — adding `Secrets SecretsBlock
  \`toml:"secrets" kind:"local"\`` should be picked up automatically without needing to hand-edit
  a hardcoded field list, pending confirmation while implementing Stage 1.

## Acceptance criteria

**Resolver registry & dispatch:**
- it should resolve a bare string or `env://<name>` source against iris's own process environment
- it should resolve a `keychain://<service>` source via `security find-generic-password -s
  <service> -w`
- it should resolve a `keychain://<service>/<account>` source via `security
  find-generic-password -s <service> -a <account> -w`
- it should resolve an `op://<vault>/<item>/<field>` source by first resolving
  `[secrets.op].bootstrap_source` into `[secrets.op].bootstrap_target`, then running `op read
  op://<vault>/<item>/<field>` with that credential set only in the `op` subprocess's own
  environment
- it should memoize a successful resolve for a given source descriptor for the life of the iris
  process, and never memoize a failed resolve

**Config schema:**
- it should accept an absent `[secrets]` block as a full no-op (no behavior change from today)
- it should reject a `[secrets.op].bootstrap_source` value that itself uses the `op` scheme, as a
  structural validation error
- it should classify `secrets` as a `kind:"local"` field, living in `.iris.local.toml` (and warn,
  not silently drop, if it appears in the shared `.iris.toml` instead — matching the existing
  local-field-in-shared-config warning)

**Wiring — all four call sites:**
- it should inject every resolvable `[secrets.env]` mapping into `RunBuild`'s subprocess
  environment
- it should inject every resolvable `[secrets.env]` mapping into `RunChecks`'s subprocess
  environment
- it should inject every resolvable `[secrets.env]` mapping into `runBuildBlock`'s subprocess
  environment (covering both `iris_reload` and `iris_publish`)
- it should inject every resolvable `[secrets.env]` mapping into `dispatchRestart`'s `exec`
  mechanism subprocess environment (covering both `iris_reload` and `iris_publish`)

**Failure visibility:**
- it should leave the target environment variable unset and log a warning naming only the
  variable and its source descriptor (never a resolved value) when a source fails to resolve, and
  this must never prevent the build/check/restart from running
- it should log a warning (not silently continue) when `.iris.local.toml` itself fails to parse,
  distinct from an individual source failing to resolve

**Subprocess safety:**
- it should treat a configured resolver command (`security`, `op`) as unresolvable via
  `exec.LookPath`, not `os.Stat` (rejecting a directory or non-executable file)
- it should kill a resolver subprocess's entire process group on timeout, not just its direct
  child
