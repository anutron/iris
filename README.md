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

## Agent-facing discoverability

A Claude session spawned inside an argus worktree sees only the `mcp__argus__iris_*` tool names and their one-line descriptions. To teach it what iris is, when to use it instead of plain `Bash`, how it composes with sibling plugins, and the intended workflows, iris ships two installable, runtime-gated assets:

- `claude/skills/iris/SKILL.md` — the agent-facing skill (the primary surface). Its frontmatter gates on being inside an argus sandbox, so the model only reaches for it there.
- `claude/snippets/iris.md` — an optional always-in-context orientation fragment for users who want a short reminder loaded every turn.

`./setup.sh` installs both as its last two steps: it symlinks the skill into `~/.claude/skills/iris` (idempotent — an existing correct symlink is left alone, any other pre-existing path is reported and not clobbered) and offers (Y/n) to append the snippet into `~/.claude/CLAUDE.md` between `# BEGIN IRIS (argus)` / `# END IRIS (argus)` markers (replaced in place on re-run). Both self-gate on `ARGUS_TASK_ID` or a cwd under `~/.argus/worktrees/`, so they stay inert outside an argus sandbox.

Install just these assets without touching the daemon:

```bash
./setup.sh --skill-only
```

The snippet keeps `tags` / `audience` frontmatter so it also slots into a snippet-compilation pipeline; `setup.sh` strips that frontmatter before appending to `CLAUDE.md`. `bash claude/install_test.sh` exercises the installer against a throwaway `HOME`.

## CLI

```
iris start --foreground            Run the daemon (called by the LaunchAgent).
iris stop                          SIGTERM to the running daemon.
iris status                        Daemon health (no args) OR self-mgmt status (with target).
iris merge-to-master <task-id>     Merge an argus task's branch into the source repo's default branch (--dry-run previews).
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
iris branch-create <task-id> <name> <base-ref>
                                   Create a branch in the source repo from an arbitrary ref (e.g. origin/master). Does NOT change the current checkout.
iris cherry-pick <task-id> <commit> <target-branch>
                                   Checkout target-branch and apply commit; aborts cleanly on conflict. Refuses default branch as target.
iris checkout <task-id> <branch> [--force]
                                   Switch the source repo to a branch. --force aborts in-progress merge/cherry-pick/rebase and discards uncommitted changes first (recovery path).
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
- `[post_merge] command = [...]` — runs after a successful `iris:merge_to_master` (or the merge step of `iris:complete_task`). Same `HookBlock` shape as `[pre_flight]` / `[verify]`: optional `working_directory` (relative to the source repo) and `timeout_seconds` (default 60). Iris exports `IRIS_TASK_ID`, `IRIS_TASK_BRANCH`, `IRIS_SOURCE_REPO`, `IRIS_DEFAULT_BRANCH`, `IRIS_MERGE_SHA` to the command's environment. Non-zero exit and timeouts are captured into the result but do NOT roll back the merge (the hook is informational).

### Restart mechanisms

- **`exit_code`** — self-only. Iris exits with `code` (default 75 = `EX_TEMPFAIL`) so the LaunchAgent's `KeepAlive: SuccessfulExit=false` respawns from the new binary.
- **`launchagent`** — requires `label`. Runs `launchctl kickstart -k gui/<uid>/<label>`.
- **`launchdaemon`** — requires `label`. Runs `launchctl kickstart -k system/<label>`. Warns if iris is not root.
- **`signal`** — requires `pid_file` and `signal` (e.g. `SIGTERM`, `SIGHUP`). Reads the PID, sends the signal.
- **`exec`** — requires `command = [...]`. Argv list, no shell. The long-tail escape hatch.
- **`none`** — does nothing for restart. For build-is-deployment workflows.

### The five verbs

- **`iris:reload`** — Pre-flight refusals (clean tree, on default branch, parsed `.iris.toml`, allowlist for cross-reload), per-source-repo lock, optional `[pre_flight]`, `git fetch && git merge --ff-only`, build, restart, optional `[verify]`. Returns a structured result and appends one line to the audit log.
- **`iris:publish`** — From an argus worktree, update the source repo's currently-checked-out branch to the worktree's HEAD, then rebuild and restart via the same `.iris.toml`. Default is ff-only; pass `--reset` for a hard reset (atomic ref + working tree). `--push` also pushes the target branch to origin (refuses the default branch, same as `iris:push`). v1.2 constraint: the target branch must equal the source repo's current branch.
- **`iris:validate_config`** — Parse and cross-validate without any side effects (no pull, build, restart, audit). Used by CI and by Claude when authoring a config.
- **`iris:ls`** — Reads the audit log and projects managed systems iris has touched recently (reloads and publishes both appear).
- **`iris:status`** — For one managed system: the resolved `.iris.toml`, current HEAD, default branch, origin SHA, working-tree-clean state, and the most recent reload/publish outcome.

### Audit log

Every reload (success and failure) appends one JSON line to `~/.iris/reload-history.jsonl`. The file is append-only and operator-rotated; iris does not rotate it.

### Self-vs-cross detection

Iris compares the resolved target source repo against `os.Executable()`'s canonical git root. If they match, the reload is self-managed (only `exit_code` is legal as restart mechanism). Otherwise it is a cross-reload (any mechanism except `exit_code` is legal).

### Why self-reload only works via MCP

The `exit_code` mechanism respawns the process that exited. When `iris reload` runs from a terminal targeting iris itself, the CLI is the process that exits – so the LaunchAgent respawns the short-lived CLI, not the daemon, and the daemon keeps running the old binary. When the same verb runs through MCP, `verbs.Reload` executes inside the daemon process, so the daemon's exit is what triggers the LaunchAgent's respawn from the new binary. Iris refuses CLI self-reload up front rather than silently no-op'ing the daemon. Use one of:

- invoke `iris_reload` via MCP from a Claude session (primary path)
- `iris reload <other-iris-managed-project>` for cross-target reloads
- `iris run-build && launchctl kickstart -k gui/$UID/<label>` to manually bounce the daemon after a build

### Iris's own `.iris.toml`

Iris ships its own `.iris.toml` at the repo root. The same six-line example above is what iris itself uses when reloading. The argus project allowlist is not consulted for self-reloads.

## `iris:merge_to_master`

Merges an argus task's branch into the source repo's default branch under the per-source-repo lock. Refuses any branch not prefixed `argus/` and any merge of the default branch into itself.

Iris does NOT clean up after the merge: the task branch and the worktree remain. Call `iris:branch_delete_remote` to remove the remote branch and let argus archive the worktree, or use `iris:complete_task` for the full ship-it sequence (merge + push default + delete remote branch + mark complete + archive).

The structured result includes:

- `sha`, `default_branch`, `task_branch`, `source_repo`, `log` — the merge outcome.
- `task_branch_still_exists` — always `true` on success. Factual; iris does not delete the branch.
- `worktree_still_present` — always `true` on success. Factual; iris does not delete the worktree.
- `post_merge` — populated when `.iris.toml` declares a `[post_merge]` block and the merge was not a dry run. Shape: `{exit_code, stdout, stderr, duration_ms, error}`. `error` is non-empty when iris could not execute the hook (timeout, binary missing). A non-zero `exit_code` is captured but does NOT roll back the merge.
- `dry_run`, `would_succeed`, `files_changed`, `conflicts` — populated only when `dry_run: true`.

`--dry-run` previews the merge: iris runs `git merge --no-commit --no-ff <branch>` under the same lock, captures `files_changed` and `conflicts`, then `merge --abort` unconditionally. No commit, no `[post_merge]` hook. `sha` is empty; `would_succeed` reports whether the real merge would land cleanly.
