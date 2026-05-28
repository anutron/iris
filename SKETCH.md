# Iris — sketch

A plugin that performs allowlisted host-side operations on behalf of sandboxed agents.

Named for Iris (Ἶρις), greek goddess of the rainbow and Hera's personal messenger. The rainbow is the visible bridge between worlds; the plugin is the typed bridge between the sandbox and the host.

- **Repo:** `~/Development/Personal/iris` (private, on GitHub as `anutron/iris`)
- **Process:** long-running user-process, started via LaunchAgent (`com.anutron.iris`)
- **Wire:** argus plugin contract (HTTP + MCP), bearer-token authenticated
- **MCP namespace:** `iris:`
- **CLI:** `iris` on PATH, with subcommands for daemon control AND direct verb invocation (debugging)
- **No settings page.** Verbs are coarse and self-contained; nothing to configure per-instance. Matches plannotator-argus's posture.

## The problem

Sandboxed agents (inside `~/.argus/worktrees/<project>/<task>/`) can't write outside the worktree. That blocks every git operation that touches the canonical source repo:

- Merging a task branch into master (master lives in the source repo, not the worktree)
- Pushing branches and tags
- Creating and merging PRs via `gh`
- Anything that mutates `~/.gitconfig`, `~/.ssh/known_hosts`, etc. in ways the sandbox blocks
- Builds that install binaries to `~/bin` or `~/.local/bin`

Today the workaround is "agent prints a command, user copies and runs it." The friction is real and recurring – it gets old by the third repetition in a single session.

## The shape

Iris is **not** a remote shell. It's an MCP tool surface with a fixed allowlist of typed verbs. Each verb is a Go function with structured arguments. There is no generic command-passthrough verb. Adding a verb is a code change plus a new release, not a config change.

Conceptually:

```
sandboxed agent ──MCP call──> argusd ──proxy──> iris ──> host shell
                                                        │
                                                        ├── ~/Development/Personal/<repo>  (git ops)
                                                        ├── ~/bin, ~/.local/bin            (install targets)
                                                        └── gh CLI, ssh keys, etc.         (credentials at rest)
```

The sandboxed agent only sees `iris:merge_to_master` as a callable tool. It does not see the underlying git commands or paths.

## Verbs (initial set)

| Verb | Args | Effect |
|---|---|---|
| `iris:merge_to_master` | `task_id`, `no_ff=true`, `message?` | In the source repo: fetch, checkout master, pull, merge the task's branch with `--no-ff`. Returns merge SHA + log. |
| `iris:push` | `task_id`, `force_with_lease=false` | In the source repo: push the task's branch to origin. |
| `iris:gh_pr_create` | `task_id`, `title`, `body`, `draft=false` | Run `gh pr create` from the source repo against the task's branch. Returns PR URL. |
| `iris:gh_pr_merge` | `task_id`, `pr_number`, `strategy=squash` | Run `gh pr merge` after CI green. |
| `iris:run_build` | `task_id`, `target?` | Run the repo's declared build script (see "Build convention" below). Stream stdout/stderr. |
| `iris:complete_task` | `task_id`, `merge_strategy=ff_merge` | Composite: merge → push → delete remote branch → delete worktree → mark argus task complete. The full "I'm done, ship it" verb. |
| `iris:branch_create` | `task_id`, `name`, `base_ref` | In the source repo: `git branch <name> <base_ref>`. Does NOT change the current checkout. Refuses default branch, leading-dash refnames, invalid git refs, and pre-existing branches. |
| `iris:cherry_pick` | `task_id`, `commit`, `target_branch` | In the source repo: checkout `target_branch`, `git cherry-pick <commit>`, abort cleanly on conflict. Refuses the default branch as target. Atomic under one lock. |
| `iris:checkout` | `task_id`, `branch`, `force=false` | In the source repo: `git checkout <branch>`. With `force=true`, aborts any in-progress merge/cherry-pick/rebase and discards uncommitted changes first — recovery from a stuck source repo. |

Verbs are deliberately coarse-grained. `iris:complete_task` is the headline verb – it captures the most common case (the agent has finished a task and wants it shipped) in a single call. Finer verbs exist for cases the composite doesn't cover.

Not in scope for v1, but documented as future verbs:

- `iris:rebase_on_master` (in the worktree, if the in-sandbox `git rebase` proves problematic)
- `iris:gh_release_create` – tagged releases
- `iris:open_in_editor` – open a path in VSCode/Cursor on the host (a kindness verb for "I made changes you should look at")

## Build convention

Iris doesn't know how a repo builds. The repo declares it:

1. **Preferred:** an executable script at `script/iris-build` in the repo root. Iris runs that script with the repo as cwd.
2. **Fallback:** if `script/iris-build` is absent but a `Makefile` exists, iris runs `make build`.
3. **Override:** the argus project record gets a new optional `build_command` field that takes precedence over both.

`iris:run_build` runs only that script, with stdout/stderr streamed back over SSE. The repo author chooses what "build" means; iris doesn't decide. This keeps the build-verb safe by construction – the agent can't tell iris what to build, only "run the build for this task."

## Safety model

The allowlist IS the safety. In addition:

- **No agent-supplied paths.** All filesystem targets are resolved from `task_id` via argus's API (`/api/tasks/:id` → worktree path → source repo path via `git rev-parse --git-common-dir`).
- **Typed args only.** No verb takes an `args` slice that gets spliced into argv. Every flag is an explicit, named parameter with a typed value.
- **Project allowlist.** Iris maintains a list of source-repo paths it's allowed to operate on, derived from argus's project list. A task whose source repo isn't in the allowlist gets a polite rejection.
- **Branch-scope guard.** `iris:merge_to_master` only merges `argus/<task-slug>` branches. It refuses to merge `master` into `master`, or random branches a clever prompt might try to coerce.
- **Kill switches:**
  - `launchctl unload` stops the iris process.
  - `argusd` revokes the bearer token → all `iris:` tools instantly fail.
  - Per-task pause: argus could add an "iris allowed?" boolean on the project record (default true).

## How a call flows end-to-end

1. Agent inside sandbox calls `iris:merge_to_master` with `task_id="abc123"` via its MCP client.
2. `argusd` receives the tool call, looks up the `iris:` namespace, forwards to iris over the plugin RPC channel (already specified in `docs/plugins.md`).
3. Iris validates: token is valid, task exists, task's source repo is in the allowlist, branch is `argus/<task-slug>`.
4. Iris resolves source repo path: `GET /api/tasks/abc123` → worktree dir → `git -C <worktree> rev-parse --git-common-dir` → the canonical source repo.
5. Iris runs the merge sequence in the source repo (under a per-source-repo mutex so concurrent merges from different tasks serialize):
   - `git fetch --all --prune`
   - `git checkout master && git pull --ff-only`
   - `git merge --no-ff argus/<task-slug>`
6. Returns `{ok: true, sha: "...", log: "..."}` to argusd → back to the agent.
7. Argus emits a task event so the TUI/PWA can show "merged".

## Install

Follows the hera pattern: a single idempotent `./setup.sh` at the repo root that does five things, prompting before each mutation.

| Step | What |
|---|---|
| 1. Build | `go build -o bin/iris ./cmd/iris` |
| 2. Install | Copy `bin/iris` to `~/bin/iris` (PATH check warns if `~/bin` not on PATH) |
| 3. State dir | `mkdir -p ~/.iris && chmod 700 ~/.iris` |
| 4. Token mint | `argus token mint --scope iris` → `~/.iris/api-token` (mode 0600) |
| 5. LaunchAgent | Write `~/Library/LaunchAgents/com.anutron.iris.plist` and `launchctl bootstrap` it |

Notes:

- **Stable symlink.** Plist runs `~/.iris/irisd` which is a symlink to the built binary. Rebuilds replace the binary in place; the plist doesn't need to be re-written.
- **KeepAlive.SuccessfulExit = false.** Restart on crash, but allow a graceful SIGTERM to stay down (so `launchctl bootout` / `iris stop` actually works).
- **Logs.** stdout/stderr captured to `~/.iris/launchd.log`.
- **Uninstall.** `./setup.sh --uninstall-launchagent` removes the plist, the symlink, and boots out the service. Keeps the token and state dir (user can delete those manually if desired).
- **Preflight.** Script bails early if `argus` or `go` isn't on PATH.

## Host-side CLI

`iris` is a single binary with subcommands:

| Subcommand | Purpose |
|---|---|
| `iris start --foreground` | Run the daemon in the foreground (LaunchAgent calls this). |
| `iris stop` | Send SIGTERM to the running daemon. |
| `iris status` | Daemon health check, token validity, registered tools, argus reachability. |
| `iris merge-to-master <task-id> [--no-ff] [-m MESSAGE]` | Direct verb invocation against the host shell. Same code path as the MCP call; bypasses argus. |
| `iris push <task-id> [--force-with-lease]` | ditto |
| `iris gh-pr-create <task-id> --title ... --body ...` | ditto |
| `iris run-build <task-id> [--target ...]` | ditto |
| `iris complete-task <task-id>` | ditto |

Direct invocation exists for two reasons: (a) debugging when something breaks at 11pm and we want to test a merge without going through argus + MCP, (b) iris is its own dogfood — implementations are pure Go functions, and the CLI wraps them with a `cobra` shim. Zero duplication.

## Open questions

- **Concurrency.** Per-source-repo mutex is enough for git ops. Builds may want their own per-repo build lock so two `iris:run_build` calls don't trample each other.
- **`iris:complete_task` failure modes.** If merge succeeds but push fails, what state are we in? The composite needs a clear "this far, no further" contract. Tentative: each sub-step is a checkpoint; failures return the checkpoint reached so a follow-up call can resume.
- **Sandbox-side discovery.** How does the agent know `iris:*` tools exist? The argus plugin contract handles this – `/api/mcp/tools` lists registered tools per session. Iris registers on argusd startup; the agent's tool inventory includes `iris:*` automatically.
- **`iris:run_build` streaming.** Confirm with a v1 prototype whether attaching directly to the calling task's terminal stream works cleanly (preferred — agent sees build output as if it ran the build itself), or whether iris needs to return a stream handle the agent polls. Argus's existing SSE plumbing supports both.

## What this is NOT

- Not a remote shell. No generic command-passthrough verb. Ever.
- Not a worktree-cleanup daemon. Argus already manages worktree lifecycle; iris just performs the privileged ops that argus's cleanup needs.
- Not a CI runner. `iris:run_build` runs a local script; it's not a substitute for GitHub Actions or local watch loops.
- Not a credential manager. Iris uses the user's existing `~/.ssh`, `~/.gitconfig`, and `gh auth` state. It doesn't store its own credentials.

## Substrate angle

The hera convo flagged that the underlying pattern ("trusted host actions for sandboxed agents") is bigger than any single plugin and could become an argus substrate primitive. That remains true – iris is the *first* concrete implementation. If a second plugin needs the same pattern, the answer is to extract `internal/hostexec` from iris into argus core, not to write a second iris. For now, iris stands alone.

## Open work

- Pick a v1 verb set (likely: `merge_to_master`, `push`, `gh_pr_create`, `run_build`, `complete_task`) and defer the rest.
- Decide on the build convention (`script/iris-build` vs `make build` vs config field).
- Sketch the bearer-token issuance flow with argusd.
- Implement.
