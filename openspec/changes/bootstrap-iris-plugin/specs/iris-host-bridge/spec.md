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

#### Scenario: Token revocation removes verbs from agent reach

- **GIVEN** argusd is the only authenticated caller of iris's MCP callbacks
- **WHEN** argusd revokes the iris scope token
- **THEN** argus garbage-collects iris's tool registrations and stops dispatching `iris:*` tool calls to iris from any sandboxed agent
- **AND** iris's own outbound calls to argus (heartbeat, GetTask, ListProjects) return 401, surfacing the revocation to the daemon log so the operator can re-mint and restart

### Requirement: Inbound callback body size limit

The MCP callback handler SHALL bound the inbound request body so a confused or buggy caller cannot exhaust daemon memory.

#### Scenario: Oversized callback body is rejected

- **WHEN** a POST to `/mcp/<tool-name>` arrives with a body larger than 1 MiB
- **THEN** the handler returns an HTTP error and does NOT invoke the tool handler

### Requirement: Source-repo path resolution from `task_id`

Every verb SHALL resolve the canonical source repo by calling argus's `GET /api/tasks/:id` to read the worktree path and then deriving the source repo via `git -C <worktree> rev-parse --git-common-dir`. Verbs SHALL NOT accept agent-supplied filesystem paths. The resolved path SHALL be canonicalized (symlinks resolved, absolute) before any comparison.

#### Scenario: Verb refuses an unknown task ID

- **WHEN** a verb is invoked with a `task_id` that argus does not recognize (404)
- **THEN** the verb returns a structured error that names the task ID and performs no filesystem mutation

#### Scenario: Verb refuses a source repo outside the project allowlist

- **GIVEN** argus exposes `GET /api/projects/full` returning the operator-curated project list
- **WHEN** the resolved source-repo path does not match any project's canonicalized `path`
- **THEN** the verb returns a structured error naming the rejected path and performs no filesystem mutation

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
- **THEN** the daemon DELETEs every registered tool before exiting (the unregister loop is bounded by a 10s deadline so a stuck argus does not block shutdown)

### Requirement: Argus-restart recovery

The daemon SHALL detect argus restarts (pid-file mtime change or socket-ping failure) and re-discover argus's dynamic REST port, then force-reregister every tool, without requiring an iris restart.

#### Scenario: Watcher detects pid-mtime change

- **WHEN** `~/.argus/daemon.pid` mtime changes
- **THEN** the daemon enters `LinkRecovering`, re-queries `Daemon.Ports` over the unix socket, updates the argus client's base URL, force-reregisters every tool, and transitions back to `LinkHealthy`

#### Scenario: Heartbeat 404 triggers recovery

- **WHEN** a heartbeat re-POST returns HTTP 404 (argus garbage-collected the registration, e.g., because argus restarted without iris noticing)
- **THEN** the registrar invokes the recovery callback as a passive fallback, restoring the link to `LinkHealthy` on success

#### Scenario: Recovery is single-flight

- **WHEN** two restart signals (pid-mtime AND ping-failure, or watcher AND heartbeat-404) fire concurrently
- **THEN** at most one recovery routine runs at a time; subsequent triggers while a recovery is in flight are coalesced

#### Scenario: Recovery sub-step failure surfaces as LinkDown

- **WHEN** any sub-step of recovery fails (ports query over the unix socket, or ForceReregister against argus)
- **THEN** the link transitions to `LinkDown`, the wrapped error is stored as `LinkLastError`, and the daemon logs the failing stage
- **AND** subsequent watcher signals retry the recovery

### Requirement: Direct CLI invocation mirrors MCP behavior

For every verb, the `iris` CLI SHALL expose a subcommand that calls the same underlying Go function as the MCP handler, against the live host shell, with the same argument contract.

#### Scenario: CLI `merge-to-master` matches the MCP call

- **WHEN** the user runs `iris merge-to-master <task-id>` from any directory
- **THEN** the daemon process is bypassed; iris calls `verbs.MergeToMaster(ctx, taskID, opts)` directly and prints the structured result
