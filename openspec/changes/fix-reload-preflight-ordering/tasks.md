## 1. Tolerant decode mode (config layer)

- [x] 1.1 Write failing tests in `internal/config/iris_toml_test.go`: `DecodeIrisTomlMode` with `LoadMode{TolerateUnknownFields: true}` turns an unknown top-level field and an unknown nested field into warnings (not `ValidationError`s) and returns a usable doc; with `LoadMode{}` (default) it still produces unknown-field `ValidationError`s (strict behavior unchanged); `schema_version` mismatch and malformed TOML remain hard errors even in tolerant mode
- [x] 1.2 Add `LoadMode` struct (`TolerateUnknownFields bool`) and `DecodeIrisTomlMode(data, sourcePath, isSelf, mode) (*IrisToml, []ValidationError, []string, error)` / `LoadIrisTomlMode(path, isSelf, mode) (...)` to `internal/config/iris_toml.go`; route `meta.Undecoded()` keys to warnings when tolerating, else to errors as today
- [x] 1.3 Reimplement existing `LoadIrisToml`/`DecodeIrisToml` as thin delegates passing `LoadMode{}`, preserving their current signatures and behavior exactly
- [x] 1.4 `go test ./internal/config/...` green; existing config + taxonomy + overlay tests still pass unchanged

## 2. Reload: pull-then-validate

- [x] 2.1 Write/adjust failing tests in `internal/verbs/reload_test.go`: validation runs against the post-pull `.iris.toml`; an additive unknown field present post-pull is tolerated (warning surfaced, reload proceeds to build); missing/malformed/schema/mechanism refusals now fire after the pull; a malformed pre-pull `.iris.toml` does not block the pull when origin fixes it; `schema_version` mismatch still hard-fails post-pull
- [x] 2.2 Add a lenient pre-pull `default_branch` peek helper (decode-and-swallow-errors, returns the override or `""`); replace the pre-pull full load used for branch resolution with it
- [x] 2.3 Reorder `Reload`: move the `.iris.toml` load+validate to after the fetch+ff-merge, using `LoadIrisTomlMode(..., LoadMode{TolerateUnknownFields: true})`; keep dirty-tree / non-default-branch / origin-reachable checks pre-pull; move the `[pre_flight]` hook to after the post-pull validation and before the build
- [x] 2.4 Thread tolerated-unknown-field warnings into `ReloadResult.Warnings` and the audit entry; ensure `pre_pull_sha`/`post_pull_sha` and the post-pull validation outcome are recorded in both success and failure audit paths
- [x] 2.5 `go test ./internal/verbs/...` green for reload

## 3. Publish: validate the worktree config

- [ ] 3.1 Write/adjust failing tests in `internal/verbs/publish_test.go`: the worktree's `.iris.toml` is the one validated (worktree-ahead config decides pass/fail); an unknown field in the worktree config is tolerated (warning, publish proceeds); an invalid worktree config is refused before the lock and before any `merge --ff-only`/`reset --hard`; `schema_version` mismatch still hard-fails
- [ ] 3.2 Change `Publish` to load+validate `filepath.Join(worktreePath, IrisTomlFilename)` via `LoadIrisTomlMode(..., LoadMode{TolerateUnknownFields: true})` instead of the source repo's `.iris.toml`; surface tolerated-field warnings into `PublishResult.Warnings` and the audit entry
- [ ] 3.3 `go test ./internal/verbs/...` green for publish

## 4. Regression coverage + full validation

- [ ] 4.1 Add a synthetic forward-compatible regression test proving the end-to-end ordering: a repo whose post-pull (reload) / worktree (publish) `.iris.toml` carries a field unknown to the running binary deploys successfully in one call, with the unknown field recorded as a warning and the build/restart reached
- [ ] 4.2 `openspec validate fix-reload-preflight-ordering --strict` green
- [ ] 4.3 `go test ./...` green and `go vet ./...` clean
- [ ] 4.4 Update each `tasks.md` checkbox as completed; re-run `openspec validate` after the last edit

## 5. Land

- [ ] 5.1 `openspec archive fix-reload-preflight-ordering --yes`
- [ ] 5.2 Push the branch via `iris_push`, merge to master via `iris_merge_to_master` (push origin/main manually if it lags)
- [ ] 5.3 `iris_reload` to deploy; confirm it succeeds cleanly (the change is to the decoder/ordering, not the toml shape, so it deploys under the old hazard rules)
