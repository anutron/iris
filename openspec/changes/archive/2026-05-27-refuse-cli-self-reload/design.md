## Context

`verbs.Reload` is a single function called by two entry points:

- `internal/mcp/handler_reload.go` (daemon-side) — sets `Caller` to the argus task_id from request context, or `"self"`.
- `cmd/iris/reload.go` (CLI-side) — hardcodes `Caller: "cli"`.

For `mechanism = "exit_code"` (the only mechanism legal for self-reload), the final step is `scheduleSelfExit(code) → os.Exit(code)`. This exits **the process that called `verbs.Reload`**. From MCP, that's the daemon — LaunchAgent's `KeepAlive{SuccessfulExit=false}` respawns it from the freshly-built binary. From CLI, that's the short-lived CLI process — the daemon is untouched and keeps running the stale binary.

The audit log currently records this CLI path as `outcome: "success"` because every step (pull, build, dispatch) returned no error from the CLI's point of view. The bug is not in any individual step; it's in the structural mismatch between "which process exits" and "which process needs to respawn."

Constraints:

- We do not want to build CLI↔daemon IPC for v1.3. That is option C in the original framing — substantial new surface (UDS, JSON-RPC, lifecycle) for an edge case.
- We do not want CLI self-reload to silently bounce the daemon via `launchctl kickstart` — that bifurcates the restart mechanism ("CLI uses launchctl, MCP uses exit_code") for the same `.iris.toml` config, which is exactly the kind of split we want to avoid.

## Goals / Non-Goals

**Goals:**

- Kill the silent-success bug: CLI self-reload must not report success when the daemon was not actually upgraded.
- Refuse as early as possible — before lock acquisition, before pull, before build. Wasted work on a doomed reload is bad UX.
- Give the operator a concrete redirect message naming all three working paths.
- Keep MCP self-reload behavior identical to today.

**Non-Goals:**

- No CLI↔daemon IPC. That's a v1.4+ conversation if it ever becomes necessary.
- No new `iris.toml` schema. The refusal is a behavior of `verbs.Reload`, not a config choice.
- No silent fallback (e.g., "since CLI can't self-reload, automatically do `launchctl kickstart`"). Explicit refusal > clever rerouting.

## Decisions

### Refuse on `caller == "cli" && isSelf`, before lock acquisition

The check goes immediately after `isSelfTarget` resolves (step 2 in `verbs.Reload`), before the pre-flight refusals at step 3 and the lock at step 4.

Rationale: a CLI self-reload is going to fail no matter what `.iris.toml` says or what the working tree looks like. Running `.iris.toml` validation, dirty-tree check, etc. first would just produce a misleading error chain — operator fixes the dirty tree, then hits "oh actually CLI self-reload is unsupported." Refuse the structural problem first.

Alternative considered: refuse *after* pre-flight, so the operator gets all the pre-flight errors first. Rejected — that conflates "your config is wrong" with "your entry point is wrong" and makes the audit log noisier.

### Discriminator is `caller == "cli"`, not "is this process the daemon?"

Tests can run `verbs.Reload` directly with any caller string. Using a process-identity check (e.g., "am I the running daemon?") would require introducing daemon-ness into the verbs package and would be untestable without spinning up a real daemon.

The CLI sets `Caller: "cli"` in exactly one place (`cmd/iris/reload.go:47`). MCP sets it to `task_id`, `"self"`, or empty (never `"cli"`). The discriminator is reliable and easy to audit.

Alternative considered: introduce a new `EntryPoint` enum on `ReloadInput`. Rejected — `Caller` already encodes the same information for audit purposes and adding a parallel field invites drift.

### Error message names all three working paths

```
self-reload from CLI is not supported: the exit_code restart mechanism
only respawns the process that exited, and the CLI is short-lived. Use
one of:
  - invoke iris_reload via MCP from a Claude session (primary path)
  - iris reload <other-iris-managed-project>  for cross-target
  - iris run-build && launchctl kickstart -k gui/$UID/<label>
    to manually bounce the daemon after a build
```

Rationale: operators hitting this error are usually in one of two situations — "my MCP cache is stale" (answer: refresh / new session) or "I'm trying to test a build without going through Claude" (answer: `iris run-build` + `launchctl kickstart`). Naming both paths covers both intents.

The `<label>` substitution is left as-is in the message (we don't read `.iris.toml` to fill it in) because the message fires before `.iris.toml` is loaded. The operator knows their label.

### Audit log records the refusal

The `AuditEntry` carries `Caller: "cli"`, `Mode: "self"`, `Outcome: "failure"`, `FailureReason: "cli-self-reload-not-supported"`. The reason string is machine-readable so future tooling can grep for this specific refusal class.

### Refusal applies to any self-targeted CLI invocation

`iris reload` (no arg), `iris reload .` (when `.` resolves to iris's own source repo), and `iris reload <task_id>` (when the task's source repo equals self) all hit the same refusal. The check happens after `isSelfTarget` so all three paths collapse to the same boolean.

## Risks / Trade-offs

- **[Risk] Operator runs `iris reload` from a terminal, gets the refusal, and is confused because the MCP path worked yesterday.** → Mitigation: error message is explicit about *why* and what to do. The README note also covers it.
- **[Risk] CI or automation scripts that wrap `iris reload` will start failing.** → Mitigation: this is the bug fix. They were silently broken before — the visible failure is the win. If anyone has such a script, they should switch to `iris run-build && launchctl kickstart` (which is what they thought was happening anyway).
- **[Trade-off] We're not adding capability, just refusing faster.** → Accepted: option C (real IPC) is the right answer for "make CLI self-reload work," but it's not v1.3 scope.

## Migration Plan

No migration needed. The refusal is purely additive on the error path. Existing successful flows (MCP self-reload, all cross-reload, `iris run-build` standalone) are unaffected.

Rollback: revert the commit. The pre-existing silent-success behavior returns. No data shape changes.

## Open Questions

- Should the README's `iris reload` section grow a small "Why self-reload only works via MCP" subsection, or is the error message itself enough? Default: yes, add the README note — the error message catches operators in the moment, but the README catches them before they try.
