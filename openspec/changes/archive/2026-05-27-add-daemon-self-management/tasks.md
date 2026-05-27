# Implementation tasks: add-daemon-self-management

**Design doc:** `openspec/changes/add-daemon-self-management/design.md`

## 1. Failing tests

- [x] 1.1 Write failing tests for `.iris.toml` parser + cross-validator (`internal/config/iris_toml_test.go`): valid file round-trips; missing schema_version; unknown schema_version; missing `[build]`; missing `[restart]`; mechanism-specific field conflicts; zero exit_code; exit_code on cross-target
- [x] 1.2 Write failing tests for audit-log writer + reader (`internal/verbs/audit_test.go`): append success entry; append failure entry; read empty file returns empty; read distinct source repos with aggregation; since-filter; limit
- [x] 1.3 Write failing tests for `internal/verbs/resolve_test.go` self-hosting discovery: `os.Executable()` symlink chain → git common-dir; both `task_id` and `path` is ambiguous; non-self-hosting verb without task_id still errors
- [x] 1.4 Write failing tests for `internal/verbs/reload_test.go`: every scenario in `specs/iris-reload/spec.md` (28 scenarios) — pre-flight refusals, pull behavior, build behavior, restart-mechanism dispatch (all six mechanisms), self-vs-cross detection, lock scope, hooks, audit log, task_id optionality, allowlist enforcement
- [x] 1.5 Write failing tests for `internal/verbs/validate_config_test.go`: every scenario in `specs/iris-validate-config/spec.md` (7 scenarios)
- [x] 1.6 Write failing tests for `internal/verbs/ls_test.go`: every scenario in `specs/iris-ls/spec.md` (7 scenarios)
- [x] 1.7 Write failing tests for `internal/verbs/status_test.go`: every scenario in `specs/iris-status/spec.md` (8 scenarios)
- [x] 1.8 Confirm every `it should X` acceptance criterion in `design.md` has a corresponding failing test (Prove-It Pattern). Iterate stage 1.x files until coverage is 100%.

## 2. `.iris.toml` parser + cross-validator

**Depends on:** Stage 1

- [x] 2.1 Add `github.com/BurntSushi/toml` to `go.mod` and `go.sum`
- [x] 2.2 Implement `internal/config/iris_toml.go`: `type IrisToml struct { SchemaVersion int; DefaultBranch string; Build BuildBlock; Restart RestartBlock; PreFlight *HookBlock; Verify *HookBlock }`. Each block as its own typed struct
- [x] 2.3 Implement `LoadIrisToml(path string) (*IrisToml, []ValidationError, error)`: read file, parse with `toml.DecodeFile`, run `Validate`. Return parse errors separately from cross-validation errors so `iris:validate_config` can surface both
- [x] 2.4 Implement `(c *IrisToml) Validate(isSelf bool) []ValidationError`: enforce required fields, schema_version == 1, mechanism-specific field exclusivity, exit_code self-only, exit_code != 0, signal name parsing, working_directory under repo root
- [x] 2.5 Implement `ValidationError` with `Field`, `Message`, `Hint`, `Line`. Wire TOML parser's line-number reports through where available
- [x] 2.6 Verify Stage 1.1 tests pass

## 3. Audit log writer + reader

**Depends on:** Stage 1

- [x] 3.1 Implement `internal/verbs/audit.go`: `type AuditEntry struct { Timestamp time.Time; Caller string; TargetSourceRepo string; Mode string; Pulled bool; PrePullSha string; PostPullSha string; BuildOutput string; RestartMechanism string; RestartOutput string; VerifyOutput string; RestartPending bool; Warnings []string; Outcome string; FailureReason string }`
- [x] 3.2 Implement `AppendAudit(entry AuditEntry) error`: O_APPEND|O_CREATE write to `~/.iris/reload-history.jsonl`. JSON-encoded line. Best-effort: log on failure, never crash caller
- [x] 3.3 Implement `ReadAudit(opts ReadOpts) ([]AuditEntry, error)`: stream-read jsonl, apply `since` and `limit` filters at read time. Return entries in append order
- [x] 3.4 Implement `AggregateBySourceRepo(entries []AuditEntry) []AggregateEntry`: dedup by `target_source_repo`, compute `total_reload_count`, `total_failure_count`, project the most-recent entry's fields onto the aggregate. Sort descending by `last_reload_at`
- [x] 3.5 Verify Stage 1.2 tests pass

## 4. Self-hosting carve-out in `verbs.Resolve`

**Depends on:** Stage 1

- [x] 4.1 Extend `internal/verbs/resolve.go` with a new `ResolveSelf() (*ResolvedRepo, error)` function: call `os.Executable()`, follow symlinks via `filepath.EvalSymlinks`, walk up to nearest `.git` directory, canonicalize the result, return `ResolvedRepo` with `WorktreePath == SourceRepo`
- [x] 4.2 Add `ResolveTarget(taskID, path string) (*ResolvedRepo, error)`: dispatches to existing `Resolve(taskID)`, new `ResolvePath(path)`, or `ResolveSelf()` based on inputs. Refuses both-set as ambiguous
- [x] 4.3 Add `ResolvePath(path string) (*ResolvedRepo, error)`: validate path exists, run `git -C path rev-parse --git-common-dir`, canonicalize. Argus project allowlist still applies (caller decides for self-hosting verbs)
- [x] 4.4 Verify Stage 1.3 tests pass

## 5. `iris:reload` verb

**Depends on:** Stages 2, 3, 4

- [x] 5.1 Implement `internal/verbs/reload.go`: `func Reload(ctx context.Context, in ReloadInput) (*ReloadResult, error)`. Inputs: `TaskID`, `Path`, `NoPull`, `TimeoutSeconds`
- [x] 5.2 Implement pre-flight: target resolution (via Stage 4.2), `.iris.toml` load + validate, working-tree-clean check, default-branch check (resolve via `git symbolic-ref refs/remotes/origin/HEAD` with `.iris.toml` override), origin reachability
- [x] 5.3 Implement pull: `git fetch origin <default>`, `git merge --ff-only origin/<default>`. Capture `pre_pull_sha`, `post_pull_sha`. Honor `no_pull`
- [x] 5.4 Implement build invocation: argv exec with `working_directory`, merged `env`, configurable timeout, kill process group on timeout, capture combined stdout+stderr
- [x] 5.5 Implement optional `[pre_flight]` hook: runs after iris pre-flight, before pull. Non-zero exit aborts with structured error
- [x] 5.6 Implement restart-mechanism dispatch: `exit_code` (defer until after response), `launchagent`, `launchdaemon`, `signal`, `exec`, `none`. Each captures output where applicable
- [x] 5.7 Implement optional `[verify]` hook: cross-reload only, runs after restart, non-zero exit returns error without rollback
- [x] 5.8 Implement self-vs-cross detection: compare resolved target to `verbs.ResolveSelf()`'s `SourceRepo`. Self gates `exit_code`-only restart; cross blocks `exit_code` at pre-flight
- [x] 5.9 Implement audit-log write: append one entry per call (success and failure), via Stage 3 helpers
- [x] 5.10 Implement self-reload exit scheduling: flush response, release lock, `time.Sleep(100ms)`, `os.Exit(code)`. Use a deferred goroutine so the MCP handler returns first
- [x] 5.11 Implement per-source-repo lock acquire/release with the existing `lockSourceRepo` helper
- [x] 5.12 Verify Stage 1.4 tests pass

## 6. `iris:validate_config` verb

**Depends on:** Stages 2, 4

- [x] 6.1 Implement `internal/verbs/validate_config.go`: `func ValidateConfig(ctx context.Context, in ValidateConfigInput) (*ValidateConfigResult, error)`
- [x] 6.2 Inputs: `TaskID`, `Path`. Same target-resolution shape as reload
- [x] 6.3 Read + parse `.iris.toml` via Stage 2.3 helpers; run cross-validation via Stage 2.4
- [x] 6.4 Return `{ valid, errors[], warnings[], resolved }`; no side effects
- [x] 6.5 Verify Stage 1.5 tests pass

## 7. `iris:ls` verb

**Depends on:** Stage 3

- [x] 7.1 Implement `internal/verbs/ls.go`: `func Ls(ctx context.Context, in LsInput) (*LsResult, error)`
- [x] 7.2 Inputs: optional `Limit` (default 50), optional `Since` (RFC3339 timestamp string)
- [x] 7.3 Read audit log via Stage 3.3; aggregate via Stage 3.4; apply filters; return entries
- [x] 7.4 Verify Stage 1.6 tests pass

## 8. `iris:status` verb

**Depends on:** Stages 2, 3, 4

- [x] 8.1 Implement `internal/verbs/status.go`: `func Status(ctx context.Context, in StatusInput) (*StatusResult, error)`
- [x] 8.2 Inputs: `TaskID`, `Path`. Same target-resolution shape as reload
- [x] 8.3 Resolve source repo (Stage 4.2); read `.iris.toml` non-fatally (capture parse error in warnings, return `config: null` on failure); capture HEAD, default-branch, origin SHA, working-tree-clean state
- [x] 8.4 Read most-recent audit entry for the resolved source repo; compute `drift` (HEAD != post_pull_sha) and `up_to_date` (HEAD == origin SHA)
- [x] 8.5 Verify Stage 1.7 tests pass

## 9. MCP handlers, Cobra subcommands, daemon registration

**Depends on:** Stages 5, 6, 7, 8

- [x] 9.1 Implement `internal/mcp/handler_reload.go`, `handler_validate_config.go`, `handler_ls.go`, `handler_status.go` following the existing handler pattern (1 MiB body cap, input schema validation, callback dispatch)
- [x] 9.2 Implement `cmd/iris/reload.go`, `validate_config.go`, `ls.go`, `status.go` following the existing direct-CLI pattern. Pretty-print JSON output
- [x] 9.3 Register the 4 new tools in `internal/daemon/run.go`'s `toolDefinitions()`. Add input schemas matching the typed Go structs
- [x] 9.4 Add `.iris.toml` to the iris repo root (six-line example from `design.md`'s schema section)
- [x] 9.5 Update `README.md` with a "Self-management" section covering the four verbs, the `.iris.toml` schema, and the audit log
- [x] 9.6 Run `make test` under `-race`; verify all stages pass
- [x] 9.7 Run `openspec validate add-daemon-self-management --strict`
- [x] 9.8 Run `bash -n setup.sh` (no behavior change but sanity check)
