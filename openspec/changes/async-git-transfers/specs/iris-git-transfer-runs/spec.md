## ADDED Requirements

### Requirement: Bounded transfer responses

The MCP `iris_push` and `iris_fetch` tools SHALL run through the shared run registry, detached from caller cancellation. They SHALL accept optional integer `wait_seconds` (default 20, clamped to 0–25). A transfer finishing within the wait SHALL return its existing success payload or classified error. Otherwise the tool SHALL return a non-error handle naming `run_id`, `tool`, `task_id`, and `status`, with instructions to poll `iris_run_result`. Status SHALL be `queued` before acquiring the source-repo lock and `running` after acquiring it. The transfer SHALL retain its iris-owned configured timeout and all existing safety checks. No authentication-wait status SHALL be inferred from elapsed time.

The handler SHALL budget preflight resolution and waiting together within 25 seconds, leaving headroom under the 30-second MCP callback deadline. If preflight fails before a transfer starts, the tool SHALL return the existing structured error.

#### Scenario: Fast transfer

- **WHEN** a push or fetch completes within the wait
- **THEN** it returns its existing payload, including `remote_sha` for push and `refs_updated` for fetch

#### Scenario: Slow transfer and polling

- **WHEN** a transfer is still executing after the wait
- **THEN** the call returns a running handle and polling that run later returns the original final payload

#### Scenario: Classified authentication failure

- **WHEN** a transfer fails with an authentication error
- **THEN** its inline or polled final result preserves `[auth_failure]`, just as timeout, network and other failures retain their classifications

### Requirement: Transfer identity and restart recovery

Duplicate in-progress MCP calls with the same task, tool and transfer options SHALL join the existing run. Push identity SHALL include destination branch, remote and force-with-lease. Results SHALL share the registry's one-hour in-memory retention. Unknown handles after a restart SHALL produce an explicit unknown-run error explaining that outcome is unknown and advising verification of remote state before retrying a transfer.

#### Scenario: Repeat call joins transfer

- **WHEN** the same push is requested while it is running
- **THEN** the second call returns the same run ID without starting a second push

#### Scenario: Restart during transfer

- **WHEN** iris restarts and a caller polls a prior transfer's run ID
- **THEN** it returns an unknown-run error, does not claim failure or success, and advises checking remote state before retrying
