# iris

An argus plugin that performs allowlisted host-side operations on behalf of sandboxed agents.

Named for Ἶρις, Hera's personal messenger and goddess of the rainbow — the visible bridge between worlds. Iris is the typed bridge between an argus sandbox and the host shell: an agent inside a worktree calls `iris:merge_to_master(task_id=...)` and iris performs the merge in the canonical source repo, then returns the structured result.

Greek-pantheon naming continues from [argus](https://github.com/drn/argus) and [hera](https://github.com/anutron/hera).

## Status

v0.1 vertical slice — `iris:merge_to_master` only. Other v1 verbs (`push`, `gh_pr_create`, `gh_pr_merge`, `run_build`, `complete_task`) land as their own OpenSpec changes.

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
iris status                        Daemon health, token validity, registered tools.
iris merge-to-master <task-id>     Direct verb invocation against the host shell.
iris push <task-id>                Push the task's branch to origin (host-side; --force-with-lease).
```

Direct invocation bypasses argus + MCP and calls the same Go function the MCP handler does — useful when debugging.
