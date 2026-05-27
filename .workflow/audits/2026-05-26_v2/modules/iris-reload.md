# iris-reload capability audit

Scope: post-archive base spec at `openspec/specs/iris-reload/spec.md` (13 requirements) vs implementation at `internal/verbs/reload.go` + helpers + entry points.

## Summary

- Requirements in base spec: 13
- Requirements implemented: 13
- Requirements with passing tests: 13
- Unimplemented spec promises: 0
- Contradictions: 0
- Uncovered-behavioral findings: 0
- Uncovered-implementation (no spec needed): 5

## Per-requirement coverage

### Requirement 1: `iris:reload` verb (overall verb behavior)

- Implementation: `internal/verbs/reload.go:84-360` (the `Reload` function), with MCP envelope at `internal/mcp/handler_reload.go:22-53` and CLI envelope at `cmd/iris/reload.go:12-59`.
- Tests: `TestReload_MCPSelfUnaffected` (lines 731-765) exercises the self-reload happy path end-to-end including `os.Exit(75)` capture; `TestReload_NoneMechanismDoesNothing` (lines 463-478) exercises a cross-reload return shape.
- Scenario coverage:
  - "Self-reload happy path": [COVERED] — `TestReload_MCPSelfUnaffected` runs with `exit_code` self-reload, asserts `Mode == "self"`, `RestartPending == true`, audit-entry written, `exitFunc(75)` captured.
  - "Cross-reload happy path with launchagent mechanism": [COVERED-PARTIAL] — `TestReload_NoneMechanismDoesNothing` and `TestReload_ExecMechanismRunsArgv` cover the cross-reload shape (`Mode == "cross"`, `RestartPending == false`, no exit), and `dispatchRestart`'s `MechanismLaunchAgent` branch at `reload.go:505-512` is the literal `launchctl kickstart -k gui/<uid>/<label>` invocation the spec describes. No dedicated `launchagent`-mechanism test (it would require stubbing `launchctl`); the underlying `runArgv` is exercised via the `exec` mechanism test, so this is a test-coverage gap on the exact argv path, not a behavioral gap.
- Notes: The MCP handler at `handler_reload.go:30-36` resolves `caller` from the request context (argus task_id), falling back to the input `task_id`, then to the literal string `"self"` when neither is set — matching the spec's note that MCP callers see "an argus task_id, the literal `'self'`, or empty — never `'cli'`."

### Requirement 2: Pre-flight refusals before any side effect

- Implementation: `reload.go:129-197` runs all pre-flight checks before the lock at line 200. The CLI-self refusal at lines 118-127 runs even earlier, before `LoadIrisToml`.
- Tests: `TestReload_RefusesDirtyTree` (line 170), `TestReload_RefusesNonDefaultBranch` (line 184), `TestReload_RefusesMissingIrisToml` (line 197), `TestReload_RefusesUnknownSchemaVersion` (line 239), `TestReload_RefusesExitCodeOnCrossTarget` (line 255), `TestReload_RefusesCrossMechanismFieldMismatch` (line 266).
- Scenario coverage:
  - "Refuses dirty working tree": [COVERED] — `TestReload_RefusesDirtyTree` writes `untracked.txt` then asserts the refusal contains "dirty" or "untracked"; `checkCleanTree` at `reload.go:385-394` is the implementation.
  - "Refuses non-default branch": [COVERED] — `TestReload_RefusesNonDefaultBranch` switches to `side-branch` and asserts the error names both branches; `reload.go:172-187` is the implementation.
  - "Refuses missing `.iris.toml`": [COVERED] — `TestReload_RefusesMissingIrisToml` asserts error contains `.iris.toml`. Implementation: `config.LoadIrisToml` returns `fs.ErrNotExist`-wrapped error which surfaces from `reload.go:131`.
  - "Refuses unknown schema_version": [COVERED] — `TestReload_RefusesUnknownSchemaVersion` sends `schema_version = 99`; validation errors collected at `reload.go:139-146`.
  - "Refuses cross-mechanism field mismatch": [COVERED] — `TestReload_RefusesCrossMechanismFieldMismatch` declares `launchagent` + `pid_file`; refused via config validation surfacing through `reload.go:139-146`.
  - "Refuses exit_code mechanism for cross-reload": [COVERED] — `TestReload_RefusesExitCodeOnCrossTarget` confirms refusal. Also defense-in-depth at `reload.go:499-501` in `dispatchRestart` (unreachable if config validation does its job, but kept).
  - "Refuses non-exit_code mechanism for self-reload": [COVERED-AT-CONFIG-LAYER] — Validation at `internal/config/iris_toml.go:334-346` (visible in the grep) rejects this. The reload-level test is absent but the layered validation matches the spec.
  - "Refuses zero exit_code": [COVERED-AT-CONFIG-LAYER] — `internal/config/iris_toml.go:323-330` rejects `code = 0`. No reload-level test, but the surface error path is shared with all schema-validation errors which IS tested.
- Notes: Spec requires "before acquiring the lock or performing any mutation." Verified at code review — all eight pre-flight scenarios fail before `lockSourceRepo` at line 200.

### Requirement 3: Pull behavior

- Implementation: `reload.go:226-270`. Fetch at line 239, `merge --ff-only` at line 249, `--no-pull` skip via `if !in.NoPull` at line 238.
- Tests: `TestReload_NoPullSkipsFetch` (line 286), `TestReload_DefaultPullsFastForward` (line 302), `TestReload_RefusesDivergentHistory` (line 326).
- Scenario coverage:
  - "Default behavior pulls": [COVERED] — `TestReload_DefaultPullsFastForward` advances `origin/main`, then runs `Reload` without `NoPull`, asserts `Pulled == true` and `PrePullSha != PostPullSha`.
  - "`no_pull` skips the pull": [COVERED] — `TestReload_NoPullSkipsFetch` runs with `NoPull: true`, asserts `Pulled == false` and `PrePullSha == PostPullSha`.
  - "Refuses divergent history": [COVERED] — `TestReload_RefusesDivergentHistory` makes local and origin diverge, asserts error contains "fast-forward". Implementation at `reload.go:249-259` captures both SHAs in the audit failure.

### Requirement 4: Build step

- Implementation: `runBuildBlock` at `reload.go:433-463`. Working dir from config at line 439, env from `mergedEnv` (line 440), timeout via `context.WithTimeout` (line 435), process-group kill on timeout (line 459).
- Tests: `TestReload_BuildSuccessIncludesOutput` (line 354), `TestReload_BuildFailureAborts` (line 371), `TestReload_BuildTimeoutKillsProcess` (line 393).
- Scenario coverage:
  - "Successful build": [COVERED] — `TestReload_BuildSuccessIncludesOutput` runs `echo hello && echo world`, asserts both strings land in `BuildOutput`.
  - "Build timeout kills the process group": [COVERED] — `TestReload_BuildTimeoutKillsProcess` runs `sleep 10` with `timeout_seconds = 1`, asserts elapsed < 5s AND error contains "timed out". The `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)` at line 459 is the process-group kill.
  - "Non-zero build exit aborts": [COVERED] — `TestReload_BuildFailureAborts` runs `exit 7`, asserts error contains "oops" (echoed output) AND that exactly one failure-outcome audit entry was written.

### Requirement 5: Restart mechanism dispatch

- Implementation: `dispatchRestart` switch at `reload.go:496-536`.
- Tests: `TestReload_ExitCodeSchedulesExit` (line 438), `TestReload_NoneMechanismDoesNothing` (line 463), `TestReload_ExecMechanismRunsArgv` (line 480), `TestReload_SignalMechanismSendsSignal` (line 497).
- Scenario coverage:
  - "exit_code dispatches to self-exit": [COVERED] — `TestReload_ExitCodeSchedulesExit` sets `code = 42`, asserts captured exit code is 42; `TestReload_MCPSelfUnaffected` defaults to 75 and asserts 75. The `releaseLock(); scheduleSelfExit(code)` sequence at `reload.go:354-358` matches the "responds to the caller, releases the lock, sleeps briefly... calls os.Exit" choreography (delay is `selfExitDelay = 100ms` in production at line 595).
  - "launchagent runs `launchctl kickstart -k gui/<uid>/<label>`": [COVERED-BY-CODE] — `reload.go:505-512` is the literal argv `launchctl kickstart -k gui/<uid>/<label>`. No dedicated test (would require stubbing `launchctl`), but the argv-runner `runArgv` is exercised by `TestReload_ExecMechanismRunsArgv`.
  - "launchdaemon warns when iris is not root": [COVERED-BY-CODE] — `reload.go:513-523` emits the warning string when `os.Geteuid() != 0` AND still runs `launchctl kickstart -k system/<label>`. No test (tests run non-root in CI; behavior would be the warning-attached path).
  - "signal reads pid_file and sends signal": [COVERED] — `TestReload_SignalMechanismSendsSignal` starts a `sleep 30` victim, writes its PID, runs `Reload` with mechanism=signal/SIGTERM, asserts the output mentions "SIGTERM" and the victim actually exits within 3s.
  - "exec runs the configured argv": [COVERED] — `TestReload_ExecMechanismRunsArgv` runs `echo restarted-via-exec`, asserts the string appears in `RestartOutput`.
  - "none is a no-op": [COVERED] — `TestReload_NoneMechanismDoesNothing` asserts `RestartOutput == ""` and `RestartPending == false`.
- Notes: Default branch (unsupported mechanism) at `reload.go:533-535` returns a structured error — defensive only, config validation rejects unknown mechanisms at parse time per the comment.

### Requirement 6: Self-vs-cross detection

- Implementation: `isSelfTarget` at `reload.go:363-371`, calls `ResolveSelf` (`resolve_self.go:24-46`) and compares via `EqualSourceRepos` (`resolve_self.go:149-154`).
- Tests: All `isSelf=true` fixtures (`reloadFixture(..., true)`) exercise the self branch; `isSelf=false` exercises cross; `TestReload_MCPSelfUnaffected` asserts `Mode == "self"`, `TestReload_NoneMechanismDoesNothing` and `TestReload_ExecMechanismRunsArgv` assert `Mode == "cross"`.
- Scenario coverage:
  - "Self when paths match": [COVERED] — Fixture installs a fake `bin/iris` and overrides `executable` so `ResolveSelf` returns the fixture repo; the comparison passes and `mode = "self"`, `RestartPending = isSelf` at `reload.go:332`.
  - "Cross when paths differ": [COVERED] — Cross fixtures point `executable` at a separate temp repo; `EqualSourceRepos` returns false; `mode = "cross"`. The exit_code-on-cross refusal scenario is tested separately by `TestReload_RefusesExitCodeOnCrossTarget`.
- Notes: `isSelfTarget` swallows `ResolveSelf` errors and returns `(false, nil)` (treats unresolved-self as cross). This is a defensive fall-through; the spec doesn't describe an unresolvable-self failure mode, so this is implementation-internal.

### Requirement 7: Per-source-repo lock spans pull, build, and restart

- Implementation: Lock acquired at `reload.go:200`, released via `defer releaseLock()` at line 208; for self+exit_code path, explicit `releaseLock()` before `scheduleSelfExit` at `reload.go:356-357`. Lock primitive: `internal/verbs/locks.go` (per-source-repo `sync.Mutex` map).
- Tests: `TestReload_LockSerializesConcurrent` (line 575) confirms two concurrent reloads on the same repo serialize (total wall time >= 500ms with build sleeping 300ms each).
- Scenario coverage:
  - "Concurrent reloads on the same source repo serialize": [COVERED] — Direct test above.
  - "Lock released before self-reload exit": [COVERED-BY-CODE] — `reload.go:356` calls `releaseLock()` before `scheduleSelfExit(code)`. The `releaseLock` closure at lines 202-207 uses a `lockHeld` guard so the deferred release is a no-op. `TestReload_MCPSelfUnaffected` exercises the full path and observes the captured exit; an explicit lock-state assertion at exit time is not in the suite but the code structure is straightforward.

### Requirement 8: Optional pre-flight and verify hooks

- Implementation: `[pre_flight]` hook at `reload.go:211-224`; `[verify]` hook at `reload.go:302-319` with `!isSelf` guard at line 303. Shared `runHook` at `reload.go:396-431`.
- Tests: `TestReload_PreFlightHookAborts` (line 417) covers the pre-flight-hook failure path; no dedicated `[verify]`-success or `[verify]`-failure test, but the runHook path is shared with `[pre_flight]`.
- Scenario coverage:
  - "Pre-flight hook aborts on non-zero exit": [COVERED] — `TestReload_PreFlightHookAborts` runs `exit 1` and asserts the captured "blocker" output appears in the error.
  - "Verify hook reports failure without rollback": [COVERED-BY-CODE] — `reload.go:306-318` returns the verify error without invoking any rollback. No restart is undone (there's no rollback code at all). Test gap on the exact verify-failure scenario, but the structural promise matches.
  - "Verify is skipped for self-reload": [COVERED-BY-CODE] — `if !isSelf && doc.Verify != nil` at line 303 is the explicit guard.

### Requirement 9: Audit log

- Implementation: `writeAudit` at `reload.go:609-611` wraps `AppendAuditBestEffort` (`audit.go:125-132`); the latter logs failures at WARN and swallows. 11 call sites in `Reload` cover every failure point + one success. CLI-self-reload audit at `reload.go:119-126`.
- Tests: `TestReload_AuditWrittenOnSuccessAndFailure` (line 532), `TestReload_BuildFailureAborts` (line 371, asserts exactly one failure entry), `TestReload_MCPSelfUnaffected` (asserts one success entry), the three `TestReload_CLISelf*_Refused` tests (each asserts one failure entry with the `cli-self-reload-not-supported` token via `assertCLISelfReloadRefused`).
- Scenario coverage:
  - "Success writes an audit entry": [COVERED] — `TestReload_AuditWrittenOnSuccessAndFailure` (success branch) and `TestReload_MCPSelfUnaffected` both assert success entries; populated fields match the spec listing (timestamp via `AppendAudit:101-103`, caller, target_source_repo, mode, pulled, pre_pull_sha, post_pull_sha, build_output, restart_mechanism, restart_output, verify_output, restart_pending, warnings, outcome).
  - "Failure also writes an audit entry": [COVERED] — same test, failure branch. Asserts `Outcome == "failure"` AND `FailureReason != ""`. Also `assertCLISelfReloadRefused` checks the refusal token in `failure_reason`.
  - "Audit-write failure does not crash the reload": [COVERED-BY-CODE] — `AppendAuditBestEffort` at `audit.go:125-132` is the literal "log and swallow" implementation. No fault-injection test but the structure is one if/log.
- Notes: The spec scenario lists field names without `pre_flight_output`. The audit entry includes `pre_flight_output` (`audit.go:39`); this is an additive field beyond the spec, additive is fine.

### Requirement 10: `task_id` is optional for `iris:reload`

- Implementation: `ResolveTarget` switch at `resolve_self.go:92-103`. The four branches are `task_id && path`, `task_id`, `path`, neither.
- Tests: `TestReload_AmbiguousBothInputs` (line 565); the `Caller: "cli"` cross-reload path-resolution is covered by `TestReload_CLICrossUnaffected` and `TestReload_NoneMechanismDoesNothing`; `task_id` resolution via `Resolve` is covered by `TestReload_CLISelfTaskID_Refused` (which sets `TaskID: "fake-task-self"` and uses the fixture's `/api/tasks/` stub at lines 115-119). No-arg self-resolution is covered by `TestReload_CLISelfNoArg_Refused` and `TestReload_MCPSelfUnaffected`.
- Scenario coverage:
  - "No-arg call defaults to self": [COVERED] — `TestReload_CLISelfNoArg_Refused` and `TestReload_MCPSelfUnaffected` both call with empty TaskID/Path; both hit `ResolveSelf` and resolve to the fixture's self repo.
  - "`task_id` resolves like other verbs": [COVERED] — `TestReload_CLISelfTaskID_Refused` exercises the `task_id` branch via the fixture's `/api/tasks/<id>` stub.
  - "`path` resolves via git common-dir": [COVERED] — Many tests pass `Path: src`; `ResolvePath` at `resolve_self.go:54-79` calls `sourceRepoFromWorktree` which runs `git rev-parse --git-common-dir` (per `resolve.go:99-108`).
  - "Both `task_id` and `path` is ambiguous": [COVERED] — `TestReload_AmbiguousBothInputs` asserts the error.

### Requirement 11: Argus project allowlist enforcement for cross-reload only

- Implementation: `Resolve` and `ResolvePath` both call `assertAllowlisted` (per `resolve_self.go:69`). `ResolveSelf` (`resolve_self.go:24-46`) does NOT — there is no allowlist call in `ResolveSelf`.
- Tests: `TestReload_PathAndAllowlistEnforcedForCross` (line 611) confirms an empty/wrong allowlist refuses a cross-reload with an error containing "allowlist". No explicit "self-reload skips allowlist" test, but every self-reload test uses an allowlist that DOESN'T include the self target (or for `TestReload_CLISelf*_Refused`, the canonical path IS in the fixture allowlist anyway — but the no-allowlist path is the structurally important one).
- Scenario coverage:
  - "Cross-reload to non-allowlisted repo is refused": [COVERED] — `TestReload_PathAndAllowlistEnforcedForCross`.
  - "Self-reload skips allowlist": [COVERED-BY-CODE] — `ResolveSelf` does not invoke `assertAllowlisted`; the `default:` branch of `ResolveTarget` calls `ResolveSelf` directly. The CLI `cmd/iris/reload.go:36-41` even handles the missing-argus-client case for pure self-reload (matching the spec's note that allowlist is not needed there).

### Requirement 12: Direct CLI invocation mirrors MCP behavior

- Implementation: CLI entry point at `cmd/iris/reload.go:12-59`. Calls the same `verbs.Reload` with `Caller: "cli"` (line 47); positional argument classification via `classifyTarget` (`cmd/iris/status.go:97-105`). Self-vs-cross detection and refusal happen identically inside `verbs.Reload`.
- Tests: `TestReload_CLISelfNoArg_Refused` (694), `TestReload_CLISelfExplicitPath_Refused` (705), `TestReload_CLISelfTaskID_Refused` (716), `TestReload_CLICrossUnaffected` (769).
- Scenario coverage:
  - "CLI with no positional resolves target as self and is then refused": [COVERED] — `TestReload_CLISelfNoArg_Refused`.
  - "CLI positional is path when prefixed with `/`, `~`, or `.`": [COVERED] — `TestReload_CLISelfExplicitPath_Refused` passes `Path: src` (absolute path resolving to self); `classifyTarget` rule is implemented at `status.go:101-103`.
  - "CLI positional is task_id otherwise": [COVERED] — `TestReload_CLISelfTaskID_Refused` passes `TaskID: "fake-task-self"` (the bare string that `classifyTarget` would route to `task_id`).
- Notes: `classifyTarget` is shared between `iris reload`, `iris status`, and `iris validate_config` (three call sites). The CLI rule is consistent across all three.

### Requirement 13: Refuses CLI self-reload at pre-flight

- Implementation: `reload.go:118-127`. The refusal fires immediately after `isSelfTarget` and before `LoadIrisToml`/`checkCleanTree`/lock/pull/build. Error sentinel `ErrCLISelfReloadUnsupported` at `reload.go:39-46` carries the `cli-self-reload-not-supported` token and the three working alternatives. Audit entry written at lines 119-125 with `Caller = "cli"`, `Mode = "self"`, `Outcome = "failure"`, `FailureReason = ErrCLISelfReloadUnsupported.Error()`.
- Tests: `TestReload_CLISelfNoArg_Refused` (694), `TestReload_CLISelfExplicitPath_Refused` (705), `TestReload_CLISelfTaskID_Refused` (716), `TestReload_MCPSelfUnaffected` (731), `TestReload_CLICrossUnaffected` (769). Shared assertion helper `assertCLISelfReloadRefused` (line 660) verifies five invariants per refusal: error contains the token; exactly one audit entry; outcome=failure with token in failure_reason; no success entry exists; `BUILD_RAN_SENTINEL` never appears in any audit BuildOutput; `PrePullSha`/`PostPullSha` are empty (rev-parse and fetch never ran).
- Scenario coverage:
  - "No-arg CLI self-reload is refused": [COVERED] — `TestReload_CLISelfNoArg_Refused`.
  - "Explicit self path via CLI is refused": [COVERED] — `TestReload_CLISelfExplicitPath_Refused`.
  - "task_id resolving to self via CLI is refused": [COVERED] — `TestReload_CLISelfTaskID_Refused`.
  - "MCP self-reload is unaffected": [COVERED] — `TestReload_MCPSelfUnaffected` runs full self-reload with `Caller: "self"`, asserts exit 75, asserts no `cli-self-reload-not-supported` token in any audit entry.
  - "CLI cross-reload is unaffected": [COVERED] — `TestReload_CLICrossUnaffected` runs `Caller: "cli"` against a non-self path, asserts success.
- Notes: The error message in `ErrCLISelfReloadUnsupported` enumerates the three alternatives the spec mandates: invoke `iris_reload` via MCP, target a different iris-managed project, and `iris run-build && launchctl kickstart -k gui/$UID/<label>` to manually bounce. Matches verbatim.

## Unimplemented spec promises

None.

## Contradictions

None.

## Behavioral gaps (intent questions)

None.

## Uncovered-implementation (no spec needed)

- `reload.go:84-88` — `caller == ""` defaults to `"unknown"`. Defensive; both MCP handler (line 30-36) and CLI entry (line 47) always set Caller. Audit-log noise containment; no spec needed.
- `reload.go:363-370` — `isSelfTarget` swallows `ResolveSelf` errors and returns `(false, nil)` (treats unresolved-self as cross). Defensive; lets cross-reload work on machines where iris's own binary cannot be located (e.g. running from `go run`). Spec doesn't describe this fall-through; treating it as cross is the conservative choice.
- `reload.go:373-383` — `resolveDefaultBranch` falls back to `"main"` with a warning if `git symbolic-ref refs/remotes/origin/HEAD` is unset. The warning is appended to `result.Warnings`. The spec calls out "the resolved default branch" but doesn't dictate the resolution priority; the implementation order is `.iris.toml override → git origin/HEAD → "main" + warning`. Consistent with general "warn but proceed" pattern.
- `reload.go:189-197` — Origin-reachable pre-flight (`git remote get-url origin`). Defensive: catches misconfigured repos before lock acquisition. Spec doesn't enumerate this check; it's a sensible addition that fails fast.
- `reload.go:593-607` — `scheduleSelfExit` delay (`selfExitDelay = 100 * time.Millisecond`) and test-overridable `exitFunc`. Implementation detail to give the MCP response a chance to flush before the daemon exits; spec calls out "sleeps briefly to ensure the response is flushed" so this is well within the spec envelope.

## Test-coverage gaps (no spec gap)

These are scenarios the spec describes that have no dedicated test but whose behavior is straight-line in code and exercised indirectly via shared helpers:

- Cross-reload happy path with `launchagent` mechanism — `launchctl` would need stubbing; argv path is shared with the tested `exec` mechanism.
- `launchdaemon` warns when iris is not root — would require running as root in CI to test the contrapositive; the warning-string branch at `reload.go:513-523` is straightforward.
- "Refuses non-exit_code mechanism for self-reload" and "Refuses zero exit_code" — validated by `config.LoadIrisToml`, which IS exercised end-to-end in `TestReload_RefusesUnknownSchemaVersion` (same surface error path).
- `[verify]` hook failure — same `runHook` machinery as `[pre_flight]`, which IS tested.
- Audit-write failure does not crash the reload — the `AppendAuditBestEffort` swallow path is one log+return; no fault injection.

These are coverage opportunities, not behavioral gaps.
