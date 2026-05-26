## ADDED Requirements

### Requirement: Daemon lifecycle

The iris daemon SHALL run as a long-running user process under launchd, started via the `com.anutron.iris` LaunchAgent, and SHALL expose a single binary on PATH (`iris`) with subcommands for daemon control and direct verb invocation.

#### Scenario: Daemon starts in foreground

- **WHEN** `iris start --foreground` is invoked with a valid scope token at `~/.iris/api-token`
- **THEN** the daemon writes its PID to `~/.iris/iris.pid`, binds the MCP listener, registers each verb as an MCP tool with argus, and blocks on SIGINT/SIGTERM

#### Scenario: Daemon refuses to start without a scope token

- **WHEN** `iris start --foreground` is invoked and `~/.iris/api-token` is missing or empty
- **THEN** the daemon exits with a non-zero status and an actionable message naming the file and the `argus token mint --scope iris` command

#### Scenario: Graceful shutdown stays down

- **WHEN** the daemon receives SIGTERM and exits cleanly with status 0
- **THEN** the LaunchAgent does NOT restart the process (KeepAlive.SuccessfulExit = false), so `launchctl bootout` reliably stops iris

### Requirement: Scope-token authentication

The daemon SHALL authenticate every outbound request to argus with the bearer token loaded from `~/.iris/api-token`, and SHALL authenticate every inbound MCP callback against a randomly generated per-process auth header passed to argus at tool-registration time.

#### Scenario: Inbound MCP callback rejects wrong token

- **WHEN** an HTTP POST to `/mcp/<tool-name>` arrives with a missing or incorrect `Authorization` header
- **THEN** the daemon returns HTTP 401 and does NOT invoke the tool handler

#### Scenario: Token revocation disables all verbs

- **WHEN** argusd revokes the iris scope token
- **THEN** every subsequent MCP callback from argus to iris fails authentication and every `iris:` tool call from a sandboxed agent fails

### Requirement: Source-repo path resolution from `task_id`

Every verb SHALL resolve the canonical source repo by calling argus's `GET /api/tasks/:id` to read the worktree path and then deriving the source repo via `git -C <worktree> rev-parse --git-common-dir`. Verbs SHALL NOT accept agent-supplied filesystem paths.

#### Scenario: Verb refuses an unknown task ID

- **WHEN** a verb is invoked with a `task_id` that argus does not recognize (404)
- **THEN** the verb returns a structured error and performs no filesystem mutation

#### Scenario: Verb refuses a source repo outside the project allowlist

- **WHEN** the resolved source-repo path is not present in argus's project list
- **THEN** the verb returns a structured error naming the rejected path

### Requirement: Per-source-repo mutex on git operations

The daemon SHALL hold a per-source-repo mutex for the duration of any git-mutating verb so concurrent calls against the same source repo serialize cleanly.

#### Scenario: Two concurrent `merge_to_master` calls serialize

- **WHEN** two sandboxed agents call `iris:merge_to_master` for two different tasks whose source repo is the same path
- **THEN** the second call blocks until the first completes, and both return success with distinct merge SHAs

### Requirement: MCP tool registration with heartbeat

The daemon SHALL register each verb with argus via `POST /api/mcp/tools` on startup and re-register on a 5-minute heartbeat, and SHALL `DELETE /api/mcp/tools/<name>` on graceful shutdown.

#### Scenario: Tools re-register on heartbeat

- **WHEN** the heartbeat ticker fires
- **THEN** the daemon re-POSTs each tool registration so argus's idle sweep does not garbage-collect it

#### Scenario: Tools unregister on shutdown

- **WHEN** the daemon receives SIGTERM
- **THEN** the daemon DELETEs every registered tool before exiting (bounded by a 10s deadline so a stuck argus does not block shutdown)

### Requirement: Direct CLI invocation mirrors MCP behavior

For every verb, the `iris` CLI SHALL expose a subcommand that calls the same underlying Go function as the MCP handler, against the live host shell, with the same argument contract.

#### Scenario: CLI `merge-to-master` matches the MCP call

- **WHEN** the user runs `iris merge-to-master <task-id>` from any directory
- **THEN** the daemon process is bypassed; iris calls `verbs.MergeToMaster(ctx, taskID, opts)` directly and prints the structured result
