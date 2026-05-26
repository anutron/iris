## Context

Iris is the first concrete instance of the "trusted host actions for sandboxed agents" pattern surfaced during the hera build-out. The argus sandbox blocks every git op that touches the canonical source repo (master lives outside the worktree), every push, every `gh pr create`, and every install to `~/bin`. The recurring workaround — "agent prints a command, user copies and runs it" — gets old by the third repetition in a single session.

The hera plugin is the reference impl for the argus plugin contract. Iris reuses hera's argus client, MCP server, registrar, and installer template wholesale; the new code lives in `internal/verbs/` and is small.

This OpenSpec change covers only the v1 vertical slice: scaffold + `iris:merge_to_master`. Subsequent verbs each land as their own OpenSpec change so they can be reviewed in isolation.

## Goals / Non-Goals

**Goals:**

- Stand up the iris repo on GitHub (`anutron/iris`, private) with the full Go scaffold (cobra root, internal packages, Makefile, openspec layout).
- Ship one production-ready verb (`iris:merge_to_master`) end-to-end: CLI invocation (`iris merge-to-master <task-id>`) and MCP tool call (`iris:merge_to_master`) share a single Go function `verbs.MergeToMaster(ctx, taskID, opts)`.
- Ship `setup.sh` as a one-shot installer (build, install, state dir, token mint, LaunchAgent) modeled on hera's.
- Establish the safety model in code from day one: source-repo paths resolved from `task_id` (never agent-supplied), branch-scope guard refuses anything other than `argus/<task-slug>`, project allowlist derived from argus's project list, per-source-repo mutex on git ops.

**Non-Goals:**

- Implementing `iris:push`, `iris:gh_pr_create`, `iris:gh_pr_merge`, `iris:run_build`, `iris:complete_task`. Each is its own follow-up OpenSpec change.
- A settings page (iris's verbs are coarse and self-contained; nothing per-instance to configure).
- Generic command passthrough. Ever. Adding a verb is a code change + release, not a config change.
- Replacing argus's worktree-cleanup. Iris performs the privileged ops cleanup needs; it does not own lifecycle.
- A CI runner. `iris:run_build` runs a local script when added later; it is not a substitute for GitHub Actions.

## Decisions

### Wire format: HTTP + MCP via argus plugin contract

Reuse hera's pattern wholesale: bearer-token-authed HTTP server with `/mcp/<tool-name>` callback endpoints, registered with argusd via `POST /api/mcp/tools` on a 5-minute heartbeat. Argusd proxies sandboxed `iris:<verb>` tool calls into iris over HTTP.

Alternative considered: register tools directly into Claude Code's MCP space. Rejected — argus is the right gatekeeper because it owns task identity (which is how iris resolves source-repo paths) and the token revocation kill switch.

### Single binary with daemon-control AND direct-verb subcommands

`iris start --foreground` runs the daemon (called by the LaunchAgent). `iris merge-to-master <task-id>` runs the verb directly against the host shell, bypassing argus + MCP. Both call the same `verbs.MergeToMaster` Go function.

Rationale: (a) debug ergonomics — when something breaks at 11pm, running the verb directly against a known task is the fastest path to a diff between "my verb is broken" vs "the plugin contract is broken"; (b) zero duplication — the CLI wraps a Go function that the MCP handler also calls.

Alternative considered: separate `irisd` and `iris-cli` binaries. Rejected — adds packaging and PATH complexity for no win.

### No agent-supplied paths. Resolution from `task_id` only.

Every verb takes `task_id` as a typed string argument. Iris resolves the source repo by calling argus's `GET /api/tasks/:id` to get the worktree path, then `git -C <worktree> rev-parse --git-common-dir` to get the canonical `.git` dir, then strips `/worktrees/<name>` to get the source repo root.

Alternative considered: let the agent pass `repo_path` directly. Rejected — that opens an obvious prompt-injection vector ("clever agent asks iris to merge into `/Users/aaron/Development/thanx/nexus`"). Resolution-from-`task_id` is the safety floor.

### Branch-scope guard

`iris:merge_to_master` refuses to merge any branch except `argus/<task-slug>` (derived from the task record, not from agent input). Refuses to merge master into master. Refuses to operate on a source repo not in the allowlist (derived from argus's project list).

### Per-source-repo mutex

A `sync.Map[string]*sync.Mutex` keyed by source-repo absolute path. `merge_to_master` holds the mutex for the entire merge sequence so concurrent merges from different tasks against the same source repo serialize cleanly.

`run_build` (future) will likely want its own per-repo build lock; that's a separate decision when the verb lands.

### State directory: `~/.iris/`

- `api-token` (mode 0600): the scope token minted by `argus token mint --scope iris`.
- `irisd`: symlink to the built binary. Plist points at the symlink so rebuilds don't need to re-write the plist.
- `launchd.log`: stdout/stderr from the LaunchAgent.
- `iris.pid`: PID file written on daemon startup.

Directory mode 0700.

### LaunchAgent: `com.anutron.iris`

`KeepAlive.SuccessfulExit = false` — restart on crash, but a graceful SIGTERM (clean exit 0) stays down so `launchctl bootout` actually works. Mirrors hera.

### Go module: `github.com/anutron/iris`

Standard layout: `cmd/iris/`, `internal/argus/`, `internal/mcp/`, `internal/verbs/`, `internal/config/`, `internal/daemon/`. The `internal/argus/` and `internal/mcp/` packages are copy-and-trim of hera's; they should NOT be promoted to a shared library yet. If a third plugin needs them, that's the point at which extraction earns its keep.

## Risks / Trade-offs

- **[Risk] Code duplication with hera.** `internal/argus/` and `internal/mcp/` will overlap heavily with hera's equivalents → Mitigation: accept the duplication for v1. The substrate angle is real but premature — wait for a second plugin to need it before extracting.
- **[Risk] Merge conflicts mid-`merge_to_master`.** Conflicting merge against master leaves the source repo in a half-merged state → Mitigation: detect conflict via `git merge` exit code, run `git merge --abort`, return a structured error with the conflict files. Don't try to auto-resolve.
- **[Risk] LaunchAgent fails to bootstrap on a fresh machine.** Could happen if argusd isn't running or the token mint fails → Mitigation: `setup.sh` does a hard preflight (`command -v argus`, `command -v go`) and prompts before every mutation; the LaunchAgent step is gated by `argus token mint --scope iris` succeeding.
- **[Trade-off] No settings page.** Future verbs that need per-instance config (e.g., a global build-timeout default) won't have an in-argus knob. Acceptable for v1 — verbs that need config will introduce config when they land, not preemptively.

## Migration Plan

- New repo, no migration. First release is v0.1.0, tagged after `iris:merge_to_master` is dogfooded against a real argus task.
- Rollback: `setup.sh --uninstall-launchagent` removes the plist, the symlink, and boots out the service. Token and state dir are kept (user deletes manually if desired).

## Open Questions

- **`iris:complete_task` failure-state contract.** If merge succeeds but push fails, what state are we in? Tentative: each sub-step is a checkpoint; failures return the checkpoint reached so a follow-up call can resume. Defer until `complete_task` is implemented.
- **`iris:run_build` streaming UX.** Attach to the calling task's terminal stream (preferred — agent sees output as if it ran the build itself) vs return a stream handle the agent polls. Prototype both when `run_build` lands.
- **Per-repo build lock granularity.** Per-repo? Per-task? Per-host? Defer until `run_build` lands.
- **Sandbox-side discovery.** How does the agent know `iris:*` tools exist? The argus plugin contract already handles this — `/api/mcp/tools` lists registered tools per session. Verify on first dogfood.
