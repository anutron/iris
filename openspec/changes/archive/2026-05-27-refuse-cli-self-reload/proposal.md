## Why

`iris reload` invoked from a terminal with no target (= self-reload) is structurally broken under `mechanism = "exit_code"`: the CLI is a short-lived process, but the `os.Exit(75)` it eventually calls happens inside the CLI's own address space — not the long-running daemon's. The LaunchAgent watches the daemon, so the daemon keeps running the old binary while the audit log reports `outcome: "success"`. This is a silent-success bug.

The primary path (sandboxed Claude session → MCP → daemon) is unaffected: there, `verbs.Reload` runs inside the daemon process, so the exit + LaunchAgent respawn choreography works correctly. CLI self-reload from a terminal is a fallback that mostly surfaces when MCP is stale-cached. Rather than build CLI↔daemon IPC for an edge case, v1.3 refuses CLI self-reload at pre-flight with a structured error pointing the operator at the working paths.

## What Changes

- `verbs.Reload` SHALL refuse with a structured error when `caller == "cli"` AND the resolved target is self, before acquiring the lock or doing any pull/build work.
- The error message SHALL name the three working alternatives: invoke `iris_reload` via MCP, target another iris-managed project, or manually bounce the daemon with `launchctl kickstart -k gui/$UID/<label>` after running `iris run-build`.
- The audit log SHALL still receive an entry with `outcome: "failure"` and `failure_reason` naming the refusal.
- Refusal applies regardless of how the self-target was specified (no positional arg, explicit path that resolves to self, or task_id whose source repo equals self).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `iris-reload`: tightens the "Direct CLI invocation mirrors MCP behavior" requirement so CLI self-reload is refused at pre-flight, and adds an explicit "Refuses CLI self-reload" requirement under the pre-flight refusal cluster.

## Impact

- `internal/verbs/reload.go`: add a self-vs-cross check on `caller == "cli"` immediately after `isSelfTarget` resolves, before any lock acquisition. Returns a typed error (`ErrCLISelfReloadUnsupported` or similar) with the redirect message baked in.
- `internal/verbs/reload_test.go`: new test covering all three CLI-self entry paths (no arg, explicit self path, task_id resolving to self).
- `cmd/iris/reload.go`: no behavioral change — the existing JSON result printer surfaces the structured error and exits non-zero. README's `iris reload` section gains a one-paragraph note clarifying that self-reload is daemon-only.
- `openspec/specs/iris-reload/spec.md` (via archive merge): MODIFIED "Direct CLI invocation mirrors MCP behavior" requirement and ADDED "Refuses CLI self-reload" requirement.
