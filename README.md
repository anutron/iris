# iris

An argus plugin that performs allowlisted host-side operations on behalf of sandboxed agents.

Named for Ἶρις, Hera's personal messenger and goddess of the rainbow — the visible bridge between worlds. Iris is the typed bridge between an argus sandbox and the host shell: an agent inside a worktree calls `iris:merge_to_master(task_id=...)` and iris performs the merge in the canonical source repo, then returns the structured result.

Greek-pantheon naming continues from [argus](https://github.com/drn/argus) and [hera](https://github.com/anutron/hera).

## Status

The full verb set has shipped and archived: host-side git/gh operations (`merge_to_master`, `merge_to_branch`, `push`, `gh_pr_*`, `branch_*`, `cherry_pick`, `checkout`, `fetch`, `tag`, `complete_task`), build and check runners (`run_build`, `run_checks`), daemon self-management (`reload`, `publish`, `validate_config`, `ls`, `status`), and the dogfood/ship workflow (`set_dogfood`, `ship_feature`, `set_local_config`). Each verb's base spec lives under [`openspec/specs/`](./openspec/specs/); the change folders that introduced them are archived under [`openspec/changes/archive/`](./openspec/changes/archive/).

Read [`SKETCH.md`](./SKETCH.md) for the full design context. In-flight work lives in the active change folders under [`openspec/changes/`](./openspec/changes/).

## What iris is NOT

- Not a remote shell. No generic command-passthrough verb. Ever.
- Not a worktree-cleanup daemon. Argus owns lifecycle; iris performs the privileged ops cleanup needs.
- Not a CI runner. `iris:run_build` and `iris:run_checks` invoke a single repo-defined local script on demand (`script/iris-build`, `script/iris-check <check>`) in the task's worktree on the host. They let an agent build or run tests/lint without leaving the sandbox — but there are no triggers, no matrix, no parallelism, and no merge gating. They are a convenience, not a substitute for GitHub Actions; CI remains authoritative.
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

Install them with the dedicated script (also offered as the final Y/n step of `./setup.sh`):

```bash
./install-claude-skills.sh        # prompts Y/n for the skill, then for the snippet
./install-claude-skills.sh --yes  # accept both non-interactively
```

It prompts separately to (1) symlink the skill into `~/.claude/skills/iris` (idempotent — an existing correct symlink is left alone, any other pre-existing path is reported and not clobbered) and (2) append the snippet into `~/.claude/CLAUDE.md` between `# BEGIN IRIS (argus)` / `# END IRIS (argus)` markers (replaced in place on re-run; declining prints the snippet path). Both assets self-gate on `ARGUS_TASK_ID` or a cwd under `~/.argus/worktrees/`, so they stay inert outside an argus sandbox.

Undo with:

```bash
./uninstall-claude-skills.sh      # prompts Y/n to remove the symlink, then the snippet block
```

The skill symlink is only removed if it points at this repo. The snippet keeps `tags` / `audience` frontmatter so it also slots into a snippet-compilation pipeline; the installer strips that frontmatter before appending to `CLAUDE.md`. Shared logic lives in `claude/lib-claude-assets.sh`; `bash claude/install_test.sh` exercises install + uninstall against a throwaway `HOME`.

## CLI

```
iris start --foreground            Run the daemon (called by the LaunchAgent).
iris stop                          SIGTERM to the running daemon.
iris status                        Daemon health (no args) OR self-mgmt status (with target).
iris merge-to-master <task-id>     Merge an argus task's branch into the source repo's default branch (--dry-run previews).
iris merge-to-branch <task-id> <target-branch> <source-ref>
                                   Merge an arbitrary source-ref into an arbitrary long-lived target-branch and push, via a scratch worktree that never disturbs the source repo's checkout (--no-ff, -m, --dry-run). Refuses the default/protected branch as target.
iris push <task-id>                Push the task's branch to origin (host-side; --force-with-lease, --branch, --remote <name>). Failures classify as [timeout]/[auth_failure]/[network_failure]/[other_failure].
iris gh-pr-create <task-id> -t T   Create a GitHub PR via gh CLI (--title required; --body, --draft, --head, --base-repo <owner/repo>, --base <branch>).
iris gh-pr-merge <task-id> -p N    Merge a GitHub PR via gh CLI (--strategy squash|merge|rebase).
iris gh-pr-view <task-id> -p N     Read a GitHub PR's state via gh CLI (--json state/checks/reviews/...).
iris gh-pr-ready <task-id> -p N    Mark a draft PR as ready for review via gh CLI (idempotent).
iris gh-pr-comment <task-id> -p N --body B   Post a comment to a GitHub PR via gh CLI.
iris gh-pr-close <task-id> -p N    Close a GitHub PR without merging (--delete-branch optional).
iris run-build <task-id>           Run the worktree's build (script/iris-build or make build [target]).
iris run-checks <task-id> <check>  Run a repo-defined check in the worktree (script/iris-check <check>; script-only).
iris complete-task <task-id>       Composite ship-it: merge + push default + delete remote branch + mark complete + archive.
iris fetch <task-id>               Run `git fetch origin` in the source repo; returns refs whose tracking SHAs changed. Same failure classification as `push`.
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
iris set-dogfood --sha <sha> --manifest <path|json> [--task <id>]
                                   Hard-reset .iris.toml's dogfood_branch to <sha>, record the manifest, then rebuild + restart. Refuses repos without dogfood_branch.
iris ship-feature --branch <name> --via pr|pr-auto [--title T] [--body B] [--merge-method squash|merge|rebase]
                                   Ship a feature branch to origin's default branch via a GitHub PR. pr-auto also waits for CI, approves, merges, fetches, and re-composes the dogfood branch.
iris reload [target]               Live-upgrade an iris-managed daemon via .iris.toml (--no-pull, --timeout).
iris validate-config [target]      Parse + cross-validate a .iris.toml; no side effects.
iris set-local-config <task-id> --field k=v [--delete k]
                                   Write/merge per-developer fields into .iris.local.toml at the source repo root.
iris ls                            List managed systems iris has reloaded (--limit, --since).
```

Direct invocation bypasses argus + MCP and calls the same Go function the MCP handler does — useful when debugging.

### Pushing to an upstream so CI runs

When `origin` is your fork (e.g. `anutron/argus`) but you have write access to the canonical upstream (`drn/argus`), GitHub will not run CI on a cross-fork PR. The CI-gated motion is to push the branch **to the upstream** and open a **same-repo PR there**:

```
iris push <task-id> --remote upstream
iris gh-pr-create <task-id> --title "..." --base-repo drn/argus
```

- `--remote` targets any **configured** remote (a name, never a URL — iris validates it exists and never adds remotes). Defaults to `origin`.
- `--base-repo` opens a same-repo PR on that `owner/repo` and **bypasses fork auto-detection** (the head is not fork-qualified; the branch must already exist there). Without it, `iris:gh_pr_create` opens a same-repo PR on origin, or — when origin is a fork — a cross-fork PR into the upstream parent (which won't run CI).
- `--base <branch>` independently selects the target **branch** (e.g. a long-lived integration branch) within whichever repo `--base-repo`/fork-detection/origin selected. Omit it and each mode's existing default branch applies unchanged.

Both `iris push` and `iris fetch` run under iris's own `git_transfer_timeout_seconds` deadline (default 300s, configurable in `.iris.toml`), independent of the caller's request timeout. A failure names its kind so you know what to do next: `[timeout]` means iris's own deadline fired — check `iris fetch`/`iris status` before assuming success or failure rather than blindly retrying; `[auth_failure]` / `[network_failure]` mean fix credentials/connectivity before retrying; `[other_failure]` covers everything else (e.g. non-fast-forward).

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
- `dogfood_branch = "dev"` — opts the developer into `iris:set_dogfood` / `iris:ship_feature`. Unset = both verbs refuse. Must be a valid git branch name and must differ from `default_branch` (the origin-first model keeps the default branch read-only, so the dogfood branch needs a distinct ref to reset). **Lives in `.iris.local.toml`, not `.iris.toml`** — see "Local vs shared config" below.
- `ship_ci_timeout_seconds = 600` — how long `iris:ship_feature`'s `pr-auto` mode waits for the PR's CI checks before giving up. Defaults to 600; must be non-negative. **Lives in `.iris.local.toml`.**
- `git_transfer_timeout_seconds = 300` — how long `iris:push`/`iris:fetch` let a single `git push`/`git fetch` run under iris's own deadline before giving up, independent of the caller's request timeout. Defaults to 300; must be non-negative.
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

### Local vs shared config

iris reads two TOML files at the source repo root:

- `.iris.toml` — **checked in, project-wide**. Identical for every developer. The build command, restart mechanism, hook blocks, and default branch live here.
- `.iris.local.toml` — **gitignored, per-developer**. Optional overlay where personal-workflow fields live. Local values override shared values for the same field.

Field taxonomy:

| Field | File | Why |
|---|---|---|
| `schema_version` | `.iris.toml` | Schema invariant; every developer must agree on it. |
| `default_branch` | `.iris.toml` | A property of the repo, not the developer. |
| `[build]` | `.iris.toml` | The repo defines how it builds. |
| `[restart]` | `.iris.toml` | The repo defines how the daemon restarts. |
| `[pre_flight]` | `.iris.toml` | Project-wide pre-pull guard. |
| `[verify]` | `.iris.toml` | Project-wide post-restart check. |
| `[post_merge]` | `.iris.toml` | Project-wide post-merge hook. |
| `git_transfer_timeout_seconds` | `.iris.toml` | A property of the repo/remote (size, network characteristics), not the developer — same for every developer and agent pushing/fetching. |
| `dogfood_branch` | `.iris.local.toml` | Each developer composes onto their own branch (or opts out entirely). |
| `ship_ci_timeout_seconds` | `.iris.local.toml` | Personal patience threshold for `ship_feature --via pr-auto`. |

Migration: if your existing `.iris.toml` has `dogfood_branch` or `ship_ci_timeout_seconds`, move those lines to `.iris.local.toml` at the repo root. iris still reads them from the shared file with a warning, so nothing breaks mid-flight, but the warning goes away once they live in the local file. `setup.sh` adds `.iris.local.toml` to your `.gitignore` and offers to scaffold a starter file.

See [`openspec/changes/add-iris-local-toml-overlay/design.md`](./openspec/changes/add-iris-local-toml-overlay/design.md) for the rationale (merge order, why warnings instead of errors, why the taxonomy is enforced).

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

## `iris:merge_to_branch`

Merges an arbitrary `source_ref` (branch, tag, or SHA) into an arbitrary long-lived, **non-default** `target_branch` and pushes it — for integration/staging branches that repeatedly absorb whole feature branches over their lifetime. Unlike `iris:merge_to_master`, the merge runs in a **scratch `git worktree add`** in a temp directory (removed on exit), so the source repo's currently checked-out branch and working tree are never touched. Neither `target_branch` nor `source_ref` is scoped to the `argus/` prefix `iris:merge_to_master` requires of its source — but merging into the default/protected branch itself is refused (that's what `iris:merge_to_master` is for).

```
iris merge-to-branch <task-id> integration/big-feature feature-x --no-ff
iris merge-to-branch <task-id> integration/big-feature v1.2.0-rc --dry-run
```

- If the target branch's local ref doesn't track `origin` (or is stale relative to it), iris resets the scratch worktree to `origin/<target_branch>` before merging, so the branch never tracking origin doesn't produce a rejected push. If `origin/<target_branch>` doesn't exist yet, iris merges from local state and the push creates it.
- A `.iris.toml` `[post_merge]` hook is read from **`target_branch`'s own tree** (post-merge, pre-cleanup) — not the source repo's untouched checkout — with env vars `IRIS_TASK_ID`, `IRIS_SOURCE_REPO`, `IRIS_TARGET_BRANCH`, `IRIS_SOURCE_REF`, `IRIS_MERGE_SHA`.
- `--dry-run` mirrors `iris:merge_to_master`'s preview: `git merge --no-commit --no-ff` in the scratch worktree, capture `files_changed`/`conflicts`, abort. No push, no hook.

## Dogfood and ship

Two opt-in verbs cover the recurring "run a composed dev build locally" and "land a finished feature" motions. Both refuse any repo whose `.iris.toml` does not declare `dogfood_branch`.

### Origin-first invariant

Local `main` (the `default_branch`) is **read-only relative to `origin`** — it only moves via `iris:fetch`, never by being pushed. `iris:push`'s default-branch refusal is unchanged; there is no direct-push-to-main path. The dogfood branch is the mutable surface, and it lives at a distinct ref (hence `dogfood_branch != default_branch`). This keeps argus's fork-from-local-main behavior reproducible and eliminates the "unpushed main commits" failure mode.

The division of labor is **iris is dumb, the agent is smart**: iris never composes branches. The agent builds the rollup (cherry-pick / merge / rebase, conflict resolution and all) somewhere it can write, then hands iris a finished SHA plus a manifest describing what's in it.

### `iris:set_dogfood`

Atomically points the configured `dogfood_branch` at a worker-supplied SHA, persists a structured manifest, then runs the same build/restart machinery as `iris:reload` (with `--no-pull`, since the SHA is already composed).

Inputs:

- `task_id` (optional) — standard resolution; omit for iris-on-iris.
- `sha` (required) — a full commit SHA reachable in the source repo.
- `force` (optional, default `false`) — override the commit-dropping ancestry refusal described below: deploy a `sha` that is not a descendant of the current dogfood SHA anyway, with a warning.
- `manifest` (required) — what composes the SHA:

```json
{
  "base":    { "ref": "main", "sha": "abc123..." },
  "layered": [ { "name": "F2", "sha": "...", "applied": "cherry-pick" } ],
  "note":    "optional free-text from the agent"
}
```

`applied` is descriptive only — iris does not validate it. Iris stamps a `recorded_at` ISO-8601 timestamp at write time.

Behavior and safety:

- The manifest is written **before** the branch reset (durable-first). If the write fails, no git mutation occurs; if the reset then fails, the manifest is "ahead" of the branch and `iris:status` reports the drift.
- **Ancestry safety:** when the dogfood branch already exists, iris refuses a `sha` that is not a descendant of the current dogfood SHA (`previous_sha`) — deploying it would silently drop every commit reachable from `previous_sha` but not `sha`. The error names the dropped-commit count and `previous_sha`. Pass `force=true` to deploy anyway; the result then carries a prominent warning naming the same count and SHA.
- The branch is created if it does not yet exist (`previous_sha: ""`). Otherwise it is force-moved — via `git branch -f` when the dogfood branch is not checked out in any worktree, or via `git reset --hard` in that worktree when it **is** checked out (git refuses to `branch -f` a branch that's currently checked out; this worktree-guard keeps the ref, HEAD, index, and tree in agreement on a live dogfood host instead of erroring).
- iris does NOT validate that `manifest.layered[i].sha` is reachable from `sha` — the manifest is for communication, not verification.

The structured result includes `set`, `dogfood_branch`, `previous_sha`, `new_sha`, the embedded `reload` result, and `warnings`. The manifest persists as `dogfood-manifest.json` in the same per-source-repo state directory as the audit log, overwritten on each call.

### `iris:ship_feature`

Lands a feature branch on `origin`'s default branch through a GitHub PR. One motion, two modes selected by `via`:

- **`pr`** — push the branch + open a PR targeting the default branch, then stop. It never merges, fetches, or touches the dogfood branch; the worker returns to it after review.
- **`pr-auto`** — push + open PR + wait for the head commit's required CI checks + approve + merge (using `merge_method`) + fetch + re-compose the dogfood branch. If checks **fail** or **time out** (`ship_ci_timeout_seconds`, default 600), iris leaves the PR open, does NOT merge, and returns `shipped: false` with a `ci_failed` / `ci_timeout` warning. A head commit with zero checks proceeds immediately.

Inputs: `task_id` (optional), `branch` (required, must exist locally and not be the default branch), `via` (required, `pr` or `pr-auto`), `pr_title` (defaults to the branch's last commit subject), `pr_body` (optional), `merge_method` (`squash` default / `merge` / `rebase`; `pr-auto` only).

The structured result includes `shipped`, `branch`, `pr_number`, `pr_url`, `merged`, `merge_sha`, `fetched`, `recompose`, and `warnings`.

**Post-ship re-compose** (`pr-auto` only): after the merge lands, iris drops the shipped feature from the dogfood manifest and replays the remaining `layered` entries against the freshly-fetched base, in a throwaway worktree so the source checkout is undisturbed. On success it atomically updates the dogfood branch + manifest. On conflict it leaves both untouched and returns `recompose: { attempted: true, succeeded: false, conflict: { branch, message } }` so the agent can drive resolution. If no manifest exists, or the shipped branch was not in it, re-compose is skipped. The merge has already landed, so a re-compose conflict never fails the ship.

There is no `direct-merge` mode: the only "advantages" of skipping the PR are anti-features for serious work, and `pr-auto` gives the same outcome with an audit trail and a CI gate.

### `iris:status` manifest

When a `dogfood-manifest.json` exists for the source repo, `iris:status` includes it as a `dogfood` field (the full manifest, including `recorded_at`). When absent, `dogfood` is `null`. A malformed manifest does not fail the call — `dogfood` is `null` and a structured warning names the path and parse error. Reporting is side-effect-free.
