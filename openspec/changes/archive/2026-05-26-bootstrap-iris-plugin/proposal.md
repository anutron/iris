## Why

Sandboxed agents running inside argus worktrees cannot perform host-side operations that touch the canonical source repo: merging task branches to master, pushing to origin, `gh pr create`, installing built binaries to `~/bin`. Today the workaround is "agent prints a command, user copies and runs it." That friction is real and recurring; iris removes it by exposing a typed, allowlisted set of host-side verbs over the argus plugin contract.

## What Changes

- Add a new argus plugin (`iris`) implementing a fixed allowlist of typed verbs over HTTP + MCP, namespace `iris:`.
- Ship `iris:merge_to_master` as the first vertical-slice verb (resolves source repo from `task_id`, runs the merge sequence under a per-source-repo mutex, returns the merge SHA and log).
- Provide a single `iris` CLI binary with both daemon-control subcommands (`start`, `stop`, `status`) and direct verb invocation (`iris merge-to-master <task-id>`) that share the same Go function with the MCP handler.
- Ship `setup.sh` modeled on hera's installer: build the binary, install to `~/bin/iris`, create `~/.iris/` (mode 0700), mint a scope token via `argus token mint --scope iris`, and install the `com.anutron.iris` LaunchAgent.
- Document additional v1 verbs (`push`, `gh_pr_create`, `gh_pr_merge`, `run_build`, `complete_task`) as follow-ups added incrementally as their own OpenSpec changes.

## Capabilities

### New Capabilities

- `iris-host-bridge`: The plugin's overall surface — daemon lifecycle, argus plugin registration, MCP tool server, scope-token auth, source-repo path resolution from `task_id`, branch-scope and project-allowlist safety guards.
- `iris-merge-to-master`: The `iris:merge_to_master` verb specifically — input contract, branch-name guard (`argus/<task-slug>` only), merge sequence (fetch / checkout master / pull / merge --no-ff), and structured response.

### Modified Capabilities

None. Iris is a brand-new plugin; no existing argus or hera specs change.

## Impact

- **New repo:** `anutron/iris` on GitHub (private), bootstrapped in this change.
- **No argus core changes** in this change. Iris consumes argus's existing plugin contract; if a `build_command` field on the project record is needed for `iris:run_build`, that's a separate argus-side change.
- **Host state:** adds `~/.iris/` (token + LaunchAgent symlink + log), `~/bin/iris`, `~/Library/LaunchAgents/com.anutron.iris.plist`. All managed idempotently by `setup.sh`.
- **No credential storage in iris.** Reuses `~/.ssh`, `~/.gitconfig`, `gh auth` as-is.
- **Kill switches:** `launchctl bootout com.anutron.iris` and `argusd` token revocation both instantly disable the surface.
