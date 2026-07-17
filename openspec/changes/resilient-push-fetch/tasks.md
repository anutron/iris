**Design doc:** `openspec/changes/resilient-push-fetch/design.md`

## 1. Tests: config layer

- [x] 1.1 Add failing test in `internal/config/iris_toml_test.go`: `ResolvedGitTransferTimeoutSeconds()` defaults to 300 when unset
- [x] 1.2 Add failing test in `internal/config/iris_toml_test.go`: `ResolvedGitTransferTimeoutSeconds()` returns the configured value when set
- [x] 1.3 Add failing test in `internal/config/iris_toml_test.go`: `Validate()` rejects a negative `git_transfer_timeout_seconds`
- [x] 1.4 Update `internal/config/iris_toml_taxonomy_test.go`: add `git_transfer_timeout_seconds` to `TestFieldKind_ExpectedShared` and `TestSharedFields_MatchesExpectedSet`'s expected lists

## 2. Tests: classifier + timeout primitives

**Depends on:** Stage 1

- [x] 2.1 Add failing test in new `internal/verbs/git_transfer_test.go`: `classifyGitFailure` returns `GitTransferAuthFailure` for known auth-pattern stderr (table-driven, several substrings)
- [x] 2.2 Add failing test: `classifyGitFailure` returns `GitTransferNetworkFailure` for known network-pattern stderr (table-driven, several substrings)
- [x] 2.3 Add failing test: `classifyGitFailure` returns `GitTransferOtherFailure` for unrecognized stderr (e.g. non-fast-forward message)
- [x] 2.4 Add failing test: `gitTransferTimeout(sourceRepo)` returns the default duration when no `.iris.toml` is present
- [x] 2.5 Add failing test: `gitTransferTimeout(sourceRepo)` returns the configured duration when `.iris.toml` sets `git_transfer_timeout_seconds`
- [x] 2.6 Add failing test: `runGitTransfer` — cancelling the passed-in `ctx` while the subprocess is running (via a slow local hook) does NOT kill it; the operation completes successfully
- [x] 2.7 Add failing test: `runGitTransfer` — a configured timeout shorter than the operation's actual duration fires at approximately that duration and returns a `*GitTransferError{Reason: GitTransferTimeout}`; `IsGitTransferTimeout(err)` is true
- [x] 2.8 Confirm every acceptance criterion in `design.md` maps to a test (Prove-It Pattern)

## 3. Tests: Push/Fetch end-to-end

**Depends on:** Stage 2

- [x] 3.1 Add failing test in `internal/verbs/push_test.go`: cancelling the caller's context mid-push (slow pre-receive hook on the bare origin) does not kill the push; `Push` returns success
- [x] 3.2 Add failing test in `internal/verbs/push_test.go`: a source-repo `.iris.toml` with a short `git_transfer_timeout_seconds` causes `Push` to fail with a timeout-classified `*GitTransferError` at approximately the configured duration
- [x] 3.3 Add failing test in `internal/verbs/fetch_test.go`: cancelling the caller's context mid-fetch (slow `remote.origin.uploadpack` wrapper) does not kill the fetch; `Fetch` returns success
- [x] 3.4 Add failing test in `internal/verbs/fetch_test.go`: a source-repo `.iris.toml` with a short `git_transfer_timeout_seconds` causes `Fetch` to fail with a timeout-classified `*GitTransferError` at approximately the configured duration

## 4. Implementation: config

**Depends on:** Stage 1 (tests written)

- [x] 4.1 Add `GitTransferTimeoutSeconds int` (`kind:"shared"`) to `IrisToml` in `internal/config/iris_toml.go`
- [x] 4.2 Add `DefaultGitTransferTimeoutSeconds = 300` constant
- [x] 4.3 Add non-negative validation for `git_transfer_timeout_seconds` in `Validate()`
- [x] 4.4 Add `ResolvedGitTransferTimeoutSeconds()` method
- [x] 4.5 Run stage-1 config tests; confirm green

## 5. Implementation: classifier + timeout primitives

**Depends on:** Stage 2 (tests written), Stage 4

- [x] 5.1 Create `internal/verbs/git_transfer.go`: `GitTransferReason` enum (`timeout`, `auth_failure`, `network_failure`, `other_failure`), `GitTransferError` type (`Op`, `Reason`, `Timeout`, `Err`) with reason-specific `Error()` text and `Unwrap()`
- [x] 5.2 Add `classifyGitFailure(output string) GitTransferReason` with the known auth/network substring tables
- [x] 5.3 Add `gitTransferTimeout(sourceRepo string) time.Duration` — loads `.iris.toml` via `config.LoadIrisToml`, falls back to the default on any load error or when unset
- [x] 5.4 Add `runGitTransfer(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error)` — runs under `context.WithTimeout(context.WithoutCancel(ctx), timeout)`, classifies failures (timeout via `transferCtx.Err()`, else `classifyGitFailure`). Also set `cmd.WaitDelay` (discovered via the failing timeout test — without it, a killed process's orphaned grandchild (e.g. `receive-pack` mid pre-receive-hook) keeps the shared I/O pipe open and `Wait()` blocks well past the configured timeout; see design.md)
- [x] 5.5 Add `IsGitTransferTimeout(err error) bool` helper
- [x] 5.6 Run stage-2 tests; confirm green

## 6. Implementation: wire into Push/Fetch

**Depends on:** Stage 3 (tests written), Stage 5

- [x] 6.1 `internal/verbs/push.go`: route the `git push` invocation through `runGitTransfer(ctx, resolved.SourceRepo, gitTransferTimeout(resolved.SourceRepo), pushArgs...)`; leave default-branch resolution and remote validation on `ctx` unchanged (pre-mutation). Also detach the trailing rev-parse (result read-back) via `context.WithTimeout(context.WithoutCancel(ctx), postTransferReadTimeout)` — discovered via the failing context-cancellation test: a cancelled `ctx` was turning a successful push into a reported failure at the read-back step
- [x] 6.2 `internal/verbs/fetch.go`: route the `git fetch origin` invocation through `runGitTransfer` the same way; leave the pre-fetch snapshot on `ctx` unchanged (pre-mutation); detach the post-fetch snapshot the same way as push's read-back
- [x] 6.3 Run stage-3 tests; confirm green

## 7. Discoverability

**Depends on:** Stage 6

- [x] 7.1 Update the `iris_push` / `iris_fetch` MCP tool descriptions in `internal/daemon/run.go` to mention the configurable `git_transfer_timeout_seconds` and the timeout/auth/network/other classification
- [x] 7.2 Confirm `cmd/iris/push.go`, `cmd/iris/fetch.go`, `internal/mcp/handler_push.go`, `internal/mcp/handler_fetch.go` require no code changes (the classification flows through existing error formatting) — verify by `go build ./...`
- [x] 7.3 Update `README.md`'s `.iris.toml` optional-fields list and field-taxonomy table with `git_transfer_timeout_seconds`
- [x] 7.4 Update `claude/skills/iris/SKILL.md`'s `iris_push`/`iris_fetch` entries with the timeout/classification behavior

## 8. Verify

**Depends on:** Stage 7

- [x] 8.1 Run the full `go test ./...` suite; confirm green
- [x] 8.2 Run `go vet ./...` and `gofmt -l` on changed files; confirm clean
- [x] 8.3 Run `openspec validate resilient-push-fetch --strict` and `openspec validate --all --strict`; confirm clean
