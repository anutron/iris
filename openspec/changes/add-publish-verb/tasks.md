## 1. Test scaffolding

- [ ] 1.1 Add test helper in `internal/verbs/publish_test.go` that builds a worktree + source repo + bare origin against `t.TempDir()`. Reuse patterns from `reload_test.go` and `push_test.go`.
- [ ] 1.2 Add a fake `.iris.toml` writer for tests with configurable `[build]` and `[restart]` blocks.
- [ ] 1.3 Add a stub-restart helper (use mechanism `none` for most tests; mechanism `exec` with `echo` for restart-output assertions).

## 2. Core Publish verb (TDD)

- [ ] 2.1 Write failing test: ff-only happy path updates source repo HEAD, runs build, dispatches restart `none`, writes audit entry with mode=publish.
- [ ] 2.2 Write failing test: refuses dirty worktree.
- [ ] 2.3 Write failing test: refuses dirty source repo.
- [ ] 2.4 Write failing test: refuses missing `.iris.toml`.
- [ ] 2.5 Write failing test: refuses source repo not on argus project allowlist.
- [ ] 2.6 Write failing test: refuses when target branch != source repo's current HEAD.
- [ ] 2.7 Write failing test: refuses non-ancestor worktree HEAD without `--reset`.
- [ ] 2.8 Write failing test: `--reset` succeeds on diverged history (ref + working tree both move).
- [ ] 2.9 Write failing test: `--push` runs after local update; remote SHA appears in result.
- [ ] 2.10 Write failing test: `--push` refuses default branch.
- [ ] 2.11 Write failing test: build failure aborts before restart; failure audit entry written.
- [ ] 2.12 Write failing test: concurrent publish + reload on same source repo serialize.
- [ ] 2.13 Write failing test: self-publish to iris's own repo refused for `exit_code` mechanism.
- [ ] 2.14 Implement `internal/verbs/publish.go` with `Publish(ctx, *argus.Client, PublishInput) (*PublishResult, error)`, `PublishInput`, and `PublishResult` types.
- [ ] 2.15 Implement pre-flight: clean worktree, clean source repo, .iris.toml loaded + validated, branch == current HEAD, ancestor check or `--reset`.
- [ ] 2.16 Implement git update step: ff-only by default, hard reset when `--reset`.
- [ ] 2.17 Implement optional push step (matches `iris:push` guardrails: default-branch refusal).
- [ ] 2.18 Delegate build + restart to `runBuildBlock` and `dispatchRestart` from `reload.go`.
- [ ] 2.19 Write success + failure audit entries via `AppendAuditBestEffort` with `mode = "publish"`.
- [ ] 2.20 Run `make test -race` until green.

## 3. CLI wiring

- [ ] 3.1 Add `iris publish` cobra subcommand in `cmd/iris/main.go` with `--branch`, `--push`, `--reset` flags.
- [ ] 3.2 CLI prints JSON result on success, non-zero exit on failure (match other verbs' CLI behavior).
- [ ] 3.3 Add a CLI smoke test if the existing test pattern covers CLI (otherwise note as manual).

## 4. MCP wiring

- [ ] 4.1 Register `iris:publish` as an MCP tool in the daemon's tool list (mirror how `iris:reload` and `iris:push` are registered).
- [ ] 4.2 MCP handler unpacks `task_id`, `branch`, `push`, `reset` from the request and calls `verbs.Publish`.
- [ ] 4.3 Add MCP-handler test if the project already has one for similar verbs.

## 5. Documentation

- [ ] 5.1 Add `iris:publish` section to README.md alongside the v1.1 verbs.
- [ ] 5.2 Document the three flags and the v1.2 constraints (target branch must equal current HEAD; no `--no-rebuild`).

## 6. Validation gates

- [ ] 6.1 `openspec validate add-publish-verb --strict` clean.
- [ ] 6.2 `make test -race` clean.
- [ ] 6.3 `go vet ./...` clean.
- [ ] 6.4 `openspec validate --all --strict` clean.
