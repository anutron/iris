# Authoring `.iris.toml` and `.iris.local.toml`

Read this when you need to **create, edit, or debug** an iris config — i.e. you are
onboarding a repo to iris-managed reload/publish, adding a dogfood/ship setup, or
`iris_validate_config` / `iris_reload` is complaining about a field. For the daily
ship/PR verbs you do **not** need any of this; that is the main `SKILL.md`.

## The two files

Both live at the **source repo root** (the canonical clone on the host, not your
worktree). Iris reads `.iris.toml` first, then overlays the optional
`.iris.local.toml` on top.

| File | Scope | Checked in? | Holds |
| --- | --- | --- | --- |
| `.iris.toml` | project-wide, identical for everyone | **yes** | how to build + restart the daemon |
| `.iris.local.toml` | per-developer | **no — gitignore it** | your machine-specific overrides (dogfood branch, CI timeout) |

`.iris.local.toml` is an **overlay, not a fallback**: it is only consulted when
`.iris.toml` parses successfully, and it may only carry `local`-tagged fields.
Its absence is silent — never an error. Add `.iris.local.toml` to `.gitignore`.

## Field taxonomy — which field goes in which file

Every field is classified `shared` (belongs in `.iris.toml`) or `local` (belongs in
`.iris.local.toml`). Put a field in the wrong file and iris still resolves a value,
but emits a warning:

| Field | Kind → file | Meaning |
| --- | --- | --- |
| `schema_version` | shared → `.iris.toml` | **required**; only `1` is supported today |
| `default_branch` | shared → `.iris.toml` | branch reload pulls from; falls back to git's `origin/HEAD` if unset |
| `[build]` | shared → `.iris.toml` | **required**; how to compile the new binary |
| `[restart]` | shared → `.iris.toml` | **required**; how to bring the new binary into service |
| `[pre_flight]` | shared → `.iris.toml` | optional gate run before build |
| `[verify]` | shared → `.iris.toml` | optional health check after a cross-reload |
| `[post_merge]` | shared → `.iris.toml` | optional hook run after `merge_to_master` commits |
| `git_transfer_timeout_seconds` | shared → `.iris.toml` | how long a single `push`/`fetch` git transfer runs under iris's own deadline before giving up (default 300, non-negative) |
| `dogfood_branch` | local → `.iris.local.toml` | the branch your dogfood/ship loop resets; must differ from `default_branch` |
| `ship_ci_timeout_seconds` | local → `.iris.local.toml` | how long `ship_feature` waits on CI (default 600, non-negative) |

Overlay rules when the same field appears in both files:

- a **`local`** field set in `.iris.toml` is still honored (graceful migration) but
  warns `local_field_in_shared_config` with a hint to move it;
- a **`shared`** field set in `.iris.local.toml` is **ignored** (shared wins) and
  warns `shared_field_in_local_config`.

## The authoring loop

Never hand-edit blind. Iris gives you two tools:

1. **`iris_validate_config(task_id)`** — parse + cross-validate with **no side
   effects** (no pull, build, restart, or audit write). It returns
   `{ valid, errors[], warnings[], resolved{…} }`. Every error carries a
   `field`, `message`, and a remediation `hint`; malformed TOML reports a line
   number. This is your edit → validate → fix loop while writing `.iris.toml`.
2. **`iris_set_local_config(task_id, fields={…}, delete=[…])`** — the safe way to
   write `.iris.local.toml`. It refuses any non-`local` field, validates per-field
   rules (e.g. `dogfood_branch` must be a valid git ref and ≠ `default_branch`),
   merges into any existing file, and writes atomically under the source-repo lock.
   Prefer this over editing `.iris.local.toml` by hand.

`.iris.toml` itself has no dedicated writer verb — edit it in your worktree like
any other source file, commit it, and gate with `iris_validate_config`.

## `[build]` — compiling the new binary

```toml
[build]
command = ["make", "build"]   # required: argv, NOT a shell string
timeout_seconds = 600          # optional, default 600, must be >= 0
working_directory = "."        # optional, default "."; relative to repo root, must not escape it
[build.env]                    # optional extra env for the build
CGO_ENABLED = "0"
```

`command` is **argv with no shell** — no pipes, globs, `&&`, or `$VAR`. If you need
shell semantics, point `command` at a script (`["script/iris-build"]`).

## `[restart]` — bringing the new binary into service

`mechanism` is required and picks which other fields are legal. Setting a field that
belongs to a different mechanism is a hard error (that is how iris catches a
half-converted config).

| `mechanism` | Required fields | Optional fields | Use for |
| --- | --- | --- | --- |
| `exit_code` | — | `code` (non-zero int, default 75) | **iris itself only** — the LaunchAgent `KeepAlive` respawns it from the new binary |
| `launchagent` | `label` | — | a macOS user daemon under launchd; iris `kickstart`s the label |
| `launchdaemon` | `label` | — | a macOS system daemon under launchd |
| `signal` | `pid_file`, `signal` | — | a daemon that reloads on a signal (e.g. `SIGTERM`, `SIGHUP`, `SIGUSR1/2`) |
| `exec` | `command` (argv) | `timeout_seconds` (default 30) | run an explicit restart command (e.g. a `systemctl`/supervisor call) |
| `none` | — | — | build only; nothing to restart (e.g. a library or one-shot) |

**The exit_code rule cuts both ways:**

- iris's **own** repo MUST use `mechanism = "exit_code"`. Any other mechanism would
  try to restart iris mid-handler. Iris rejects it at parse time.
- Any **other** managed daemon MUST NOT use `exit_code` — it is self-only. Use
  `launchagent`, `signal`, `exec`, etc.

`iris_validate_config` needs to know whether the target is iris's own repo to apply
this rule; it derives that automatically from the resolved repo.

## Optional hooks — `[pre_flight]`, `[verify]`, `[post_merge]`

All three share the `HookBlock` shape and are argv (no shell):

```toml
[pre_flight]                       # gate run BEFORE build (reload); default timeout 60s
command = ["script/preflight"]
timeout_seconds = 60
working_directory = "."

[verify]                           # health check AFTER a cross-reload restart; default timeout 30s
command = ["script/healthcheck"]   # only runs for OTHER daemons, not iris's self-reload

[post_merge]                       # runs after iris_merge_to_master commits the merge; default timeout 60s
command = ["script/notify-merge"]
```

A non-zero hook exit fails the operation and the captured output comes back in the
verb result, so you see why.

## Worked examples

### A. Minimal config for iris itself (the canonical shape)

```toml
schema_version = 1
default_branch = "main"

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
```

### B. A macOS LaunchAgent-managed daemon (a non-iris system)

```toml
schema_version = 1
default_branch = "main"

[build]
command = ["go", "build", "-o", "bin/mydaemon", "./cmd/mydaemon"]
timeout_seconds = 300

[restart]
mechanism = "launchagent"
label = "com.example.mydaemon"

[verify]
command = ["script/healthcheck"]
```

### C. A daemon that reloads on a signal

```toml
schema_version = 1

[build]
command = ["make", "build"]

[restart]
mechanism = "signal"
pid_file = "/var/run/mydaemon.pid"
signal = "SIGHUP"
```

### D. Per-developer overlay (`.iris.local.toml`, gitignored)

```toml
dogfood_branch = "dev"
ship_ci_timeout_seconds = 900
```

Equivalent, written through the verb instead of by hand:

```
iris_set_local_config(task_id, fields={ dogfood_branch: "dev", ship_ci_timeout_seconds: 900 })
```

## Bootstrapping dogfood when the default branch has no `.iris.toml`

The from-scratch case: you want to dogfood a daemon (build a composed branch and run
it) but its default branch does not carry an `.iris.toml` yet. You are an agent in an
argus sandbox worktree of the target repo.

**The trap to avoid:** do NOT try to make `iris_validate_config` go green from the
sandbox first. It reads the **source repo's checked-out working tree**, never your
worktree — so a config you have only authored locally is invisible to it, and the
sandbox cannot write the source-root file. Authoring the config is just ordinary
committed work; let `iris_set_dogfood` validate it at build time.

1. **Author `.iris.toml`** in your worktree per the schema above, and add
   `.iris.local.toml` to `.gitignore`. Commit both as a single commit on your task
   branch — plain git in your own worktree, no special iris verb.

2. **Open the adoption PR (non-blocking).** `iris_push(task_id)` your branch and
   `iris_gh_pr_create(task_id, title="Add .iris.toml")` against the default branch.
   This is the permanent home for the shared config; its review and merge happen on
   their own timeline and MUST NOT gate dogfooding.

3. **Declare the dogfood branch.** `iris_set_local_config(task_id, fields={dogfood_branch: "dev"})`
   writes the gitignored `.iris.local.toml` to the source repo root. (`dogfood_branch`
   must differ from the default branch.)

4. **Compose the SHA to dogfood.** In your worktree, build the commit you want to run:
   `default branch + your .iris.toml commit + the feature branch(es) you are testing`
   (merge or cherry-pick; resolve conflicts yourself). Iris is dumb, the agent is
   smart — you compose, iris deploys.

5. **Deploy.** `iris_set_dogfood(task_id, sha=<composed SHA>, manifest={...})`. Iris
   points `dev` at that SHA, checks `dev` out to build it (its tree now carries
   `.iris.toml`), restarts the daemon, and restores your source repo to the branch it
   was on. **This is the validation step** — a malformed `.iris.toml` fails here with
   the structured error; there is no sandbox path to validate it earlier.

Things to keep straight:

- **You run `set_dogfood` while the source repo is on the default branch** — not on
  `dev`. `set_dogfood` reads `dogfood_branch` from `.iris.local.toml` even when the
  default branch has no `.iris.toml` yet (that file lives on `dev`), then its reload
  checks `dev` out to build. Do NOT pre-check-out `dev` yourself: reload's pre-flight
  requires HEAD to be on the default branch, so entering on `dev` is refused.
- **`dev` does not stay checked out.** `set_dogfood`'s reload checks it out only to
  build, then restores the entry branch, so your active working state on the source
  repo is preserved. The *binary* is dev; the *repo* returns to your branch.
- **The adoption PR and dogfooding are independent.** You dogfood `dev` immediately;
  the PR adding `.iris.toml` to the default branch waits for review without blocking
  anything. Once it merges, `dev` (composed from the default branch) inherits the
  config the normal way and this bootstrap is no longer needed.
- **Subsequent deploys are ancestry-checked.** The first `iris_set_dogfood` call above
  creates `dev` fresh, so nothing to check yet. Once `dev` exists, iris refuses a
  `sha` that is not a descendant of `dev`'s current SHA (it would silently drop
  commits) unless you pass `force=true` — recompose onto the current SHA instead of
  reaching for `force` as the default move.

## Common authoring mistakes

- **Shell strings in `command`.** `command = "make build"` is wrong — it must be an
  argv array `["make", "build"]`. No shell metacharacters; wrap them in a script.
- **`exit_code` on a non-iris daemon** (or any non-`exit_code` mechanism on iris
  itself). Both are rejected at parse time — see the exit_code rule above.
- **Mixing restart fields.** e.g. `mechanism = "launchagent"` plus a stray
  `pid_file`. Each mechanism owns its fields; a foreign field is an error.
- **`dogfood_branch` in `.iris.toml`** (warns; move it to `.iris.local.toml`) or
  **equal to `default_branch`** (hard error — the origin-first model keeps the
  default branch read-only, so the dogfood branch needs a distinct name like `dev`).
- **Absolute or escaping `working_directory`.** It must be relative to the repo root
  and must not climb out with `..`.
- **Forgetting to gitignore `.iris.local.toml`.** It is per-developer; never commit it.
- **Editing without validating.** Run `iris_validate_config` after every change; for
  local fields, prefer `iris_set_local_config` so per-field rules are enforced for you.
