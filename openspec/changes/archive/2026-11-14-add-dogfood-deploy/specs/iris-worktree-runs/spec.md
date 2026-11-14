## ADDED Requirements

### Requirement: Host deployment run handles

iris_dogfood_deploy, iris_set_dogfood and iris_reload SHALL run through the shared RunRegistry, accepting wait_seconds defaulting to 20 and clamped to 0–25. Slow calls SHALL return a non-error run_id/status handle, continue independently of caller cancellation, and be pollable through iris_run_result. Identical active requests SHALL share a run; differing deployment arguments SHALL not. Finished deployment errors SHALL preserve structured readiness evidence when available. Self-deployment SHALL finish source-lock and lease-flight cleanup before scheduling the exit_code restart. Self-reload SHALL warn that in-memory handles disappear when iris exits, and SHALL report restart_pending rather than verified readiness.

#### Scenario: Slow deploy returns pollable handle

- **WHEN** a deploy outlasts wait_seconds
- **THEN** the call returns a run handle and polling later returns its deployment result

#### Scenario: Caller disconnects

- **WHEN** the deploy caller disconnects during build
- **THEN** the registered operation continues and records its outcome

#### Scenario: Self-reload loses memory

- **WHEN** iris schedules its own exit_code restart
- **THEN** its result warns that the restart loses run handles and readiness remains pending
