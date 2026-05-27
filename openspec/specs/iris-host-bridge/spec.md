# iris-host-bridge Specification

## Purpose
TBD - created by archiving change bootstrap-iris-plugin. Update Purpose after archive.
## Requirements
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
- **THEN** the handler returns HTTP 400 or 413 and does NOT invoke the tool handler

### Requirement: Source-repo path resolution from `task_id`

Every verb SHALL resolve the canonical source repo by calling argus's `GET /api/tasks/:id` to read the worktree path and then deriving the source repo via `git -C <worktree> rev-parse --git-common-dir`. Verbs SHALL NOT accept agent-supplied filesystem paths. Both the resolved source-repo path AND the worktree path SHALL be canonicalized (symlinks resolved, absolute) before any comparison or use as a lock key.

A narrow exception applies to **self-hosting verbs**: verbs whose normal operating mode is to act on iris itself (the running daemon's own source repo) MAY omit `task_id`. When invoked without `task_id` (and without an alternative explicit target such as `path`), a self-hosting verb SHALL discover its source repo via `os.Executable()` symlink resolution followed by a walk-up to the nearest `.git` directory. The result SHALL be canonicalized. The set of self-hosting verbs SHALL be enumerated in this requirement; verbs not in the list SHALL continue to require `task_id`.

In v1.1, the self-hosting verbs are: `iris:reload`, `iris:validate_config`, `iris:ls`, `iris:status`. All other verbs continue to require `task_id` as before.

#### Scenario: Verb refuses an unknown task ID

- **WHEN** a verb is invoked with a `task_id` that argus does not recognize (404)
- **THEN** the verb returns a structured error that names the task ID and performs no filesystem mutation

#### Scenario: Verb refuses a source repo outside the project allowlist

- **GIVEN** argus exposes `GET /api/projects/full` returning the operator-curated project list
- **WHEN** the resolved source-repo path does not match any project's canonicalized `path`
- **THEN** the verb returns a structured error naming the rejected path and performs no filesystem mutation

#### Scenario: Resolved paths are canonical on both sides

- **GIVEN** macOS resolves `/var` to `/private/var` via a system symlink
- **WHEN** argus stores the task's `worktree_path` as `/var/folders/.../wt` (non-canonical) and the source repo's working tree resolves to `/private/var/folders/.../src`
- **THEN** `verbs.Resolve` returns both `WorktreePath` and `SourceRepo` in their canonical (symlink-resolved, absolute) form so per-worktree and per-source-repo lock keys collide reliably for the same physical path

#### Scenario: Self-hosting verb without task_id discovers via os.Executable

- **GIVEN** the iris daemon is running as `/Users/aaron/.iris/irisd`, a symlink to `/Users/aaron/Development/Personal/iris/bin/iris`
- **WHEN** a self-hosting verb (e.g., `iris:reload`) is invoked with no `task_id`
- **THEN** iris resolves `os.Executable()`, follows the symlink chain to `/Users/aaron/Development/Personal/iris/bin/iris`, walks up to find the nearest `.git` directory at `/Users/aaron/Development/Personal/iris/.git`, and uses `/Users/aaron/Development/Personal/iris` (canonicalized) as the source repo

#### Scenario: Non-self-hosting verb still refuses missing task_id

- **WHEN** a verb not on the self-hosting list (e.g., `iris:push`, `iris:merge_to_master`) is invoked without `task_id`
- **THEN** the verb returns a structured error stating `task_id` is required, even if iris's own source repo would otherwise be derivable

#### Scenario: Self-hosting verb with both task_id and self-discovery inputs is ambiguous

- **WHEN** a self-hosting verb is invoked with both `task_id` and `path` set (where `path` is the optional explicit target)
- **THEN** the verb returns a structured error and resolves nothing

#### Scenario: Self-hosting verbs skip argus project allowlist for self-targets

- **WHEN** a self-hosting verb resolves its source repo via `os.Executable()` (self-target)
- **THEN** the verb does NOT consult argus's `/api/projects/full` (iris's own repo is implicit; the verb is constrained by being self-hosting, not by the allowlist)
- **AND** when the same verb is invoked with `task_id` or `path` resolving to a NON-self source repo, the argus allowlist enforcement applies as for any other verb

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

