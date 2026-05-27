## 1. Test scaffolding

- [x] 1.1 Add test helper in `internal/verbs/publish_test.go` that builds a worktree + source repo + bare origin against `t.TempDir()`. Reuse patterns from `reload_test.go` and `push_test.go`.
- [x] 1.2 Add a fake `.iris.toml` writer for tests with configurable `[build]` and `[restart]` blocks.
- [x] 1.3 Add a stub-restart helper (use mechanism `none` for most tests; mechanism `exec` with `echo` for restart-output assertions).

## 2. Core Publish verb (TDD)

- [x] 2.1 Write failing test: ff-only happy path updates source repo HEAD, runs build, dispatches restart `none`, writes audit entry with mode=publish.
- [x] 2.2 Write failing test: refuses dirty worktree.
- [x] 2.3 Write failing test: refuses dirty source repo.
- [x] 2.4 Write failing test: refuses missing `.iris.toml`.
- [x] 2.5 Write failing test: refuses source repo not on argus project allowlist.
- [x] 2.6 Write failing test: refuses when target branch != source repo's current HEAD.
- [x] 2.7 Write failing test: refuses non-ancestor worktree HEAD without `--reset`.
- [x] 2.8 Write failing test: `--reset` succeeds on diverged history (ref + working tree both move).
- [x] 2.9 Write failing test: `--push` runs after local update; remote SHA appears in result.
- [x] 2.10 Write failing test: `--push` refuses default branch.
- [x] 2.11 Write failing test: build failure aborts before restart; failure audit entry written.
- [x] 2.12 Write failing test: concurrent publish + reload on same source repo serialize.
- [x] 2.13 Write failing test: self-publish to iris's own repo refused for `exit_code` mechanism.
- [x] 2.14 Implement `internal/verbs/publish.go` with `Publish(ctx, *argus.Client, PublishInput) (*PublishResult, error)`, `PublishInput`, and `PublishResult` types.
- [x] 2.15 Implement pre-flight: clean worktree, clean source repo, .iris.toml loaded + validated, branch == current HEAD, ancestor check or `--reset`.
- [x] 2.16 Implement git update step: ff-only by default, hard reset when `--reset`.
- [x] 2.17 Implement optional push step (matches `iris:push` guardrails: default-branch refusal).
- [x] 2.18 Delegate build + restart to `runBuildBlock` and `dispatchRestart` from `reload.go`.
- [x] 2.19 Write success + failure audit entries via `AppendAuditBestEffort` with `mode = "publish"`.
- [x] 2.20 Run `make test -race` until green.

## 3. CLI wiring

- [x] 3.1 Add `iris publish` cobra subcommand in `cmd/iris/main.go` with `--branch`, `--push`, `--reset` flags.
- [x] 3.2 CLI prints JSON result on success, non-zero exit on failure (match other verbs' CLI behavior).
- [x] 3.3 Add a CLI smoke test if the existing test pattern covers CLI (otherwise note as manual).

## 4. MCP wiring

- [x] 4.1 Register `iris:publish` as an MCP tool in the daemon's tool list (mirror how `iris:reload` and `iris:push` are registered).
- [x] 4.2 MCP handler unpacks `task_id`, `branch`, `push`, `reset` from the request and calls `verbs.Publish`.
- [x] 4.3 Add MCP-handler test if the project already has one for similar verbs.

## 5. Documentation

- [x] 5.1 Add `iris:publish` section to README.md alongside the v1.1 verbs.
- [x] 5.2 Document the three flags and the v1.2 constraints (target branch must equal current HEAD; no `--no-rebuild`).

## 6. Validation gates

- [x] 6.1 `openspec validate add-publish-verb --strict` clean.
- [x] 6.2 `make test -race` clean.
- [x] 6.3 `go vet ./...` clean.
- [x] 6.4 `openspec validate --all --strict` clean.
