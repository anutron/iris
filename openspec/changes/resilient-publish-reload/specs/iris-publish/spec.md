## ADDED Requirements

### Requirement: Publish owns cancellation after pre-flight

After acquiring the source-repo lock and checking caller cancellation, publish SHALL detach the mutation-through-restart sequence from caller cancellation and deadlines before resetting or fast-forwarding the checkout. Git commands SHALL remain bounded by Iris's configured git transfer timeout, and build and restart SHALL retain their separate configured timeouts. Genuine failures SHALL still abort with a terminal failure audit entry; a failed build SHALL NOT restart.

#### Scenario: Caller cancels during build after reset or fast-forward

- **WHEN** the caller cancels while publish is building after a successful reset or fast-forward
- **THEN** the build continues, restart dispatch runs on build success, and the terminal audit record includes the applied SHA and restart outcome

#### Scenario: Cancelled queued caller does not mutate

- **WHEN** the caller is already cancelled when publish acquires the lock
- **THEN** publish fails before any checkout mutation and records the cancellation

#### Scenario: Build timeout still aborts

- **WHEN** the build exceeds its configured timeout after caller cancellation
- **THEN** publish records a build failure and does not attempt restart

### Requirement: Publish persists explicit restart outcome

Terminal publish audit entries SHALL include `restart_attempted` and `restart_succeeded` booleans. Attempted SHALL indicate entry into restart dispatch; succeeded SHALL indicate dispatch returned successfully, including a configured no-op. Failure before dispatch SHALL leave both false. Restart failure SHALL set attempted true and succeeded false and retain the error and output.

#### Scenario: Restart fails after caller cancellation

- **WHEN** a detached publish build succeeds and the restart command fails
- **THEN** the terminal audit contains `outcome = "failure"`, `restart_attempted = true`, `restart_succeeded = false`, and the restart failure reason
