# iris

An argus plugin that performs allowlisted host-side operations on behalf of sandboxed agents.

Named for Ἶρις, Hera's personal messenger and goddess of the rainbow — the visible bridge between worlds. Iris is the typed bridge between an argus sandbox and the host shell: an agent inside a worktree calls `iris:merge_to_master(task_id=...)` and iris performs the merge in the canonical source repo, then returns the structured result.

Greek-pantheon naming continues from [argus](https://github.com/drn/argus) and [hera](https://github.com/anutron/hera).

## Status

v1 verb set landed: `merge_to_master`, `push`, `gh_pr_create`, `gh_pr_merge`, `run_build`, `complete_task`. Each verb lives in its own OpenSpec change folder under `openspec/changes/` until they archive together.

Read [`SKETCH.md`](./SKETCH.md) for the full design context. Read [`openspec/changes/bootstrap-iris-plugin/`](./openspec/changes/bootstrap-iris-plugin/) for the active change folder.

## What iris is NOT

- Not a remote shell. No generic command-passthrough verb. Ever.
- Not a worktree-cleanup daemon. Argus owns lifecycle; iris performs the privileged ops cleanup needs.
- Not a CI runner. `iris:run_build` runs a local script when added; it's not a substitute for GitHub Actions.
- Not a credential manager. Reuses `~/.ssh`, `~/.gitconfig`, `gh auth` as-is.

## Install

```bash
./setup.sh
```

The installer is idempotent. It builds the binary, copies it to `~/bin/iris`, creates `~/.iris/` (mode 0700), mints a scope token via `argus token mint --scope iris`, and bootstraps the `com.anutron.iris` LaunchAgent.

Uninstall the LaunchAgent only:

```bash
./setup.sh --uninstall-launchagent
```

## CLI

```
iris start --foreground            Run the daemon (called by the LaunchAgent).
iris stop                          SIGTERM to the running daemon.
iris status                        Daemon health (no args) OR self-mgmt status (with target).
iris merge-to-master <task-id>     Merge an argus task's branch into the source repo's default branch.
iris push <task-id>                Push the task's branch to origin (host-side; --force-with-lease).
iris gh-pr-create <task-id> -t T   Create a GitHub PR via gh CLI (--title required; --body, --draft).
iris gh-pr-merge <task-id> -p N    Merge a GitHub PR via gh CLI (--strategy squash|merge|rebase).
iris gh-pr-view <task-id> -p N     Read a GitHub PR's state via gh CLI (--json state/checks/reviews/...).
iris gh-pr-ready <task-id> -p N    Mark a draft PR as ready for review via gh CLI (idempotent).
iris gh-pr-comment <task-id> -p N --body B   Post a comment to a GitHub PR via gh CLI.
iris gh-pr-close <task-id> -p N    Close a GitHub PR without merging (--delete-branch optional).
iris run-build <task-id>           Run the worktree's build (script/iris-build or make build [target]).
iris complete-task <task-id>       Composite ship-it: merge + push default + delete remote branch + mark complete + archive.
iris fetch <task-id>               Run `git fetch origin` in the source repo; returns refs whose tracking SHAs changed.
iris branch-delete-remote <task-id> --branch <name>
                                   Delete a remote branch on origin (refuses the default branch).
iris tag <task-id> --tag <name> [--message "..."]
                                   Create an annotated tag at origin/<default-branch> and push it to origin.
iris reload [target]               Live-upgrade an iris-managed daemon via .iris.toml (--no-pull, --timeout).
iris validate-config [target]      Parse + cross-validate a .iris.toml; no side effects.
iris ls                            List managed systems iris has reloaded (--limit, --since).
```

Direct invocation bypasses argus + MCP and calls the same Go function the MCP handler does — useful when debugging.

## Self-management

Iris can live-upgrade any daemon it is asked to manage (including itself) via four verbs that share a declarative convention.

### The `.iris.toml` convention

Place a `.iris.toml` at the source repo root. The minimum useful file is six lines:

```toml
schema_version = 1
default_branch = "main"

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
```

Required:

- `schema_version = 1` — iris refuses files with a missing or unknown version.
- `[build] command = [...]` — argv list (no shell), run in the source repo root by default.
- `[restart] mechanism = "..."` — one of `exit_code`, `launchagent`, `launchdaemon`, `signal`, `exec`, `none`.

Optional:

- `default_branch = "main"` — overrides `git symbolic-ref refs/remotes/origin/HEAD`.
- `[build] timeout_seconds`, `working_directory`, `env` — knobs for the build step.
- `[pre_flight] command = [...]` — runs after iris's built-in pre-flight refusals, before pull. Non-zero exit aborts.
- `[verify] command = [...]` — runs after restart (cross-reload only). Non-zero exit reports failure but does NOT roll back.

### Restart mechanisms

- **`exit_code`** — self-only. Iris exits with `code` (default 75 = `EX_TEMPFAIL`) so the LaunchAgent's `KeepAlive: SuccessfulExit=false` respawns from the new binary.
- **`launchagent`** — requires `label`. Runs `launchctl kickstart -k gui/<uid>/<label>`.
- **`launchdaemon`** — requires `label`. Runs `launchctl kickstart -k system/<label>`. Warns if iris is not root.
- **`signal`** — requires `pid_file` and `signal` (e.g. `SIGTERM`, `SIGHUP`). Reads the PID, sends the signal.
- **`exec`** — requires `command = [...]`. Argv list, no shell. The long-tail escape hatch.
- **`none`** — does nothing for restart. For build-is-deployment workflows.

### The four verbs

- **`iris:reload`** — Pre-flight refusals (clean tree, on default branch, parsed `.iris.toml`, allowlist for cross-reload), per-source-repo lock, optional `[pre_flight]`, `git fetch && git merge --ff-only`, build, restart, optional `[verify]`. Returns a structured result and appends one line to the audit log.
- **`iris:validate_config`** — Parse and cross-validate without any side effects (no pull, build, restart, audit). Used by CI and by Claude when authoring a config.
- **`iris:ls`** — Reads the audit log and projects managed systems iris has reloaded recently.
- **`iris:status`** — For one managed system: the resolved `.iris.toml`, current HEAD, default branch, origin SHA, working-tree-clean state, and the most recent reload outcome.

### Audit log

Every reload (success and failure) appends one JSON line to `~/.iris/reload-history.jsonl`. The file is append-only and operator-rotated; iris does not rotate it.

### Self-vs-cross detection

Iris compares the resolved target source repo against `os.Executable()`'s canonical git root. If they match, the reload is self-managed (only `exit_code` is legal as restart mechanism). Otherwise it is a cross-reload (any mechanism except `exit_code` is legal).

### Iris's own `.iris.toml`

Iris ships its own `.iris.toml` at the repo root. The same six-line example above is what iris itself uses when reloading. The argus project allowlist is not consulted for self-reloads.
