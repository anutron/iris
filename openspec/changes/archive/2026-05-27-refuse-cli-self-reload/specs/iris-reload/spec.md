## ADDED Requirements

### Requirement: Refuses CLI self-reload at pre-flight

When `verbs.Reload` is invoked from the CLI entry point (i.e. `Caller == "cli"`) and the resolved target source repo is iris's own source repo, iris SHALL refuse with a structured error immediately after self-vs-cross detection — before acquiring the per-source-repo lock, before any pre-flight refusals on `.iris.toml` or working tree state, and before any pull or build.

The error message SHALL name the three working alternatives: invoking `iris_reload` via MCP, targeting a different iris-managed project, and manually running `iris run-build` followed by `launchctl kickstart -k gui/$UID/<label>` to bounce the daemon.

The refusal SHALL still write an audit-log entry with `caller = "cli"`, `mode = "self"`, `outcome = "failure"`, and `failure_reason` containing a stable, grep-friendly token (e.g. `cli-self-reload-not-supported`).

This requirement exists because `mechanism = "exit_code"` exits the process that called `verbs.Reload`. From MCP that process is the long-running daemon and the LaunchAgent respawns it from the new binary; from the CLI that process is the short-lived `iris` CLI and the daemon is untouched. Silent success (today's behavior) misleads the operator into thinking the daemon was upgraded when it was not.

#### Scenario: No-arg CLI self-reload is refused

- **GIVEN** the operator runs `iris reload` in a terminal with no positional argument
- **WHEN** the request reaches `verbs.Reload` with `Caller = "cli"` and self-vs-cross detection classifies the target as self
- **THEN** iris returns a structured error containing the token `cli-self-reload-not-supported` and the three redirect options, does NOT acquire the per-source-repo lock, does NOT pull, does NOT build, does NOT dispatch any restart, and writes an audit entry with `outcome = "failure"` and the same reason token

#### Scenario: Explicit self path via CLI is refused

- **GIVEN** the operator runs `iris reload <path>` where `<path>` resolves to iris's own source repo
- **WHEN** the request reaches `verbs.Reload` with `Caller = "cli"` and self-vs-cross detection classifies the target as self
- **THEN** iris returns the same structured error as the no-arg case, with no lock acquired and no side effects, and writes the same audit entry

#### Scenario: task_id resolving to self via CLI is refused

- **GIVEN** the operator runs `iris reload <task_id>` where the argus task's source repo equals iris's own source repo
- **WHEN** the request reaches `verbs.Reload` with `Caller = "cli"` and self-vs-cross detection classifies the target as self
- **THEN** iris returns the same structured error, with no lock acquired and no side effects, and writes the same audit entry

#### Scenario: MCP self-reload is unaffected

- **GIVEN** a Claude session inside an argus sandbox invokes `iris_reload` with no `task_id` and no `path`
- **WHEN** the request reaches `verbs.Reload` with `Caller` set from MCP request context (an argus task_id, the literal `"self"`, or empty — never `"cli"`)
- **THEN** iris runs the full reload flow as before (pre-flight, lock, pull, build, restart dispatch, audit, scheduled exit), and the new refusal does NOT fire

#### Scenario: CLI cross-reload is unaffected

- **GIVEN** the operator runs `iris reload <target>` from a terminal and the target resolves to a source repo that is NOT iris's own
- **WHEN** the request reaches `verbs.Reload` with `Caller = "cli"` and self-vs-cross detection classifies the target as cross
- **THEN** iris runs the full cross-reload flow as before (allowlist check, pre-flight, lock, pull, build, restart dispatch, optional verify hook, audit), and the new refusal does NOT fire

## MODIFIED Requirements

### Requirement: Direct CLI invocation mirrors MCP behavior

The `iris reload [target] [--no-pull]` CLI SHALL call the same `verbs.Reload` Go function as the MCP handler, against the live host shell, with the same structured result — EXCEPT that self-reload from the CLI is refused at pre-flight (see "Refuses CLI self-reload at pre-flight"). CLI cross-reload remains a full mirror of MCP cross-reload.

#### Scenario: CLI with no positional resolves target as self and is then refused

- **WHEN** the user runs `iris reload` from any shell with no positional argument
- **THEN** iris resolves the target via `os.Executable()` (matching the MCP flow), then refuses the CLI self-reload at pre-flight per "Refuses CLI self-reload at pre-flight"

#### Scenario: CLI positional is path when prefixed with `/`, `~`, or `.`

- **WHEN** the user runs `iris reload /some/path` or `iris reload ./repo`
- **THEN** iris treats the argument as a filesystem path; if it resolves to self, the CLI self-reload refusal applies; otherwise the cross-reload flow runs as before

#### Scenario: CLI positional is task_id otherwise

- **WHEN** the user runs `iris reload 17798305...`
- **THEN** iris treats the argument as an argus task_id; if the task's source repo equals self, the CLI self-reload refusal applies; otherwise the cross-reload flow runs as before
