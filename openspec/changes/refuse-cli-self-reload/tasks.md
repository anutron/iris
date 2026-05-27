## 1. Tests first

- [x] 1.1 Add `TestReload_CLISelfNoArg_Refused` in `internal/verbs/reload_test.go` — calls `verbs.Reload` with `Caller: "cli"`, no `TaskID`, no `Path`; asserts the returned error contains the token `cli-self-reload-not-supported`, asserts no audit entry with `outcome: "success"` was written, asserts an audit entry with `outcome: "failure"` and the reason token was written, asserts no `git fetch` or build command was executed (use the existing test harness's command observer).
- [x] 1.2 Add `TestReload_CLISelfExplicitPath_Refused` — same as 1.1 but with `Path` set to the test's faked self source-repo root.
- [x] 1.3 Add `TestReload_CLISelfTaskID_Refused` — same as 1.1 but with `TaskID` set to a fake argus task whose source repo equals the faked self root (use the test argus client).
- [x] 1.4 Add `TestReload_MCPSelfUnaffected` — invokes `verbs.Reload` with `Caller: "self"` (or an argus task_id), no `TaskID`, no `Path`; asserts the refusal does NOT fire and the existing self-reload happy path runs through to `scheduleSelfExit` (using the existing `exitFunc` override).
- [x] 1.5 Add `TestReload_CLICrossUnaffected` — invokes `verbs.Reload` with `Caller: "cli"` and a `Path` resolving to a non-self test repo; asserts the cross-reload flow runs through restart dispatch.
- [x] 1.6 Run the suite: tests 1.1–1.5 fail (the refusal does not exist yet).

## 2. Implementation

- [x] 2.1 In `internal/verbs/reload.go`, define a sentinel error variable `ErrCLISelfReloadUnsupported = errors.New("cli-self-reload-not-supported: ...")` whose message text matches the three-line redirect block in the design doc.
- [x] 2.2 In `verbs.Reload`, immediately after the `isSelfTarget` resolution (between current step 2 and step 3), branch on `caller == "cli" && isSelf`: write the audit entry with `Mode: "self"`, `Outcome: "failure"`, `FailureReason` containing the token `cli-self-reload-not-supported`, then return `(nil, ErrCLISelfReloadUnsupported)`.
- [x] 2.3 Run the test suite: tests 1.1–1.5 now pass.

## 3. Documentation

- [ ] 3.1 Update `README.md`'s `iris reload` section with a short subsection titled "Why self-reload only works via MCP" explaining the structural reason (one paragraph) and pointing at the three working alternatives.
- [ ] 3.2 Cross-check the SKETCH.md / STATUS.md if they reference `iris reload` self-flow; tighten any wording that implies CLI self-reload works.

## 4. Validation

- [ ] 4.1 Run `openspec validate refuse-cli-self-reload --strict`; resolve any reported issues.
- [ ] 4.2 Run the full test suite (`go test ./...`) and confirm everything passes.
- [ ] 4.3 Manually verify: run `iris reload` from a terminal in this worktree's parent iris install; confirm the structured error appears, no `git fetch` happens, no build runs, and the audit log shows the failure entry. (Do this in the source repo, not the sandbox, since the test is about CLI-from-terminal behavior.)
