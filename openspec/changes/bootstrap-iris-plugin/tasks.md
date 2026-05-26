## 1. Repo bootstrap

- [x] 1.1 `git init` (already done by argus task scaffold)
- [x] 1.2 Initial commit on `main` (already done)
- [x] 1.3 Create `anutron/iris` on GitHub via `gh repo create` (already done)
- [x] 1.4 Install spec-check pre-commit hook symlink
- [x] 1.5 `openspec init --tools claude`
- [x] 1.6 Copy `iris-sketch.md` into the worktree as `SKETCH.md`
- [x] 1.7 README stub explaining what iris is and pointing at SKETCH.md

## 2. Go scaffold

- [x] 2.1 `go mod init github.com/anutron/iris` targeting Go 1.25
- [x] 2.2 Add `github.com/spf13/cobra` dependency
- [x] 2.3 `Makefile` with `build`, `test`, `fmt`, `vet`, `clean`, `install-dev` targets (mirror hera)
- [x] 2.4 `cmd/iris/main.go` cobra root with subcommands stubbed
- [x] 2.5 `internal/config/config.go` — `Config`, `Default()`, `TokenPath()`, `PIDPath()`, `LogPath()`, `LoadToken()`, `EnsureStateDir()`
- [x] 2.6 `internal/argus/client.go` — typed HTTP client, bearer-token auth, `doJSON`
- [x] 2.7 `internal/argus/tasks.go` — `GetTask(ctx, taskID) (*Task, error)`
- [x] 2.8 `internal/argus/mcp.go` — `RegisterTool`, `UnregisterTool`
- [x] 2.9 `internal/mcp/server.go` — HTTP listener on `/mcp/<name>` with bearer-token auth
- [x] 2.10 `internal/mcp/envelope.go` — `CallbackEnvelope`, `Response`, `TextResponse`, `ErrorResponse`
- [x] 2.11 `internal/mcp/registrar.go` — register-on-start, 5-minute heartbeat, unregister-on-stop

## 3. Verb implementation: `merge_to_master`

- [x] 3.1 `internal/verbs/resolve.go` — source-repo path resolution from `task_id` (argus.GetTask → worktree → `git rev-parse --git-common-dir` → source repo root) and project-allowlist check
- [x] 3.2 `internal/verbs/locks.go` — per-source-repo mutex map
- [x] 3.3 `internal/verbs/merge_to_master.go` — `MergeToMaster(ctx, taskID, opts) (*MergeResult, error)`:
  - resolve source repo + task branch
  - branch-scope guard (must be `argus/<task-slug>`, never master/main)
  - hold source-repo mutex
  - run `git fetch --all --prune`, `git checkout master`, `git pull --ff-only`, `git merge --no-ff argus/<task-slug>`
  - on conflict: `git merge --abort`, return structured error
  - on success: return `{Sha, Log}`
- [x] 3.4 Unit tests for `merge_to_master` using temp git repos (no network, no LaunchAgent)

## 4. CLI + MCP wiring

- [x] 4.1 `cmd/iris/start.go` — daemon entrypoint that calls `internal/daemon.Run`
- [x] 4.2 `cmd/iris/status.go` — daemon health, token validity, registered tools, argus reachability
- [x] 4.3 `cmd/iris/stop.go` — SIGTERM to the running daemon via PID file
- [x] 4.4 `cmd/iris/merge_to_master.go` — direct verb subcommand calling `verbs.MergeToMaster`
- [x] 4.5 `internal/daemon/run.go` — assemble argus client, MCP server, registrar; register the `iris_merge_to_master` tool; serve until ctx cancellation
- [x] 4.6 `internal/mcp/handler_merge_to_master.go` — decode envelope input, call `verbs.MergeToMaster`, encode response

## 5. Installer

- [x] 5.1 `setup.sh` — port from hera (preflight, build, install to ~/bin, state dir, token mint, LaunchAgent)
- [x] 5.2 Verify `--uninstall-launchagent` flow works

## 6. Validation + dogfood

- [x] 6.1 `openspec validate --all --strict` passes
- [x] 6.2 `make build` succeeds; `iris status` runs (with or without daemon)
- [x] 6.3 `make test` passes (unit tests for verb + resolver)
- [ ] 6.4 Open PR for the bootstrap commit
- [ ] 6.5 (Deferred to follow-up) dogfood `iris:merge_to_master` against a real argus task once setup.sh has been run

## 7. Followups (documented, not gated on this change)

These verbs are intentionally out of scope for the bootstrap. Each lands as its own OpenSpec change folder when implemented; they are listed here so a reader landing on this change folder sees the roadmap without scrolling design.md.

- ~~`iris-push`~~ (added in `add-push-verb` change)
- ~~`iris-gh-pr-create`~~ (added in `add-gh-pr-create-verb` change)
- ~~`iris-gh-pr-merge`~~ (added in `add-gh-pr-merge-verb` change)
- ~~`iris-run-build`~~ (added in `add-run-build-verb` change)
- `iris-complete-task`
