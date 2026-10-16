## ADDED Requirements

### Requirement: Reload owns cancellation after pre-flight

After acquiring the source-repo lock and checking caller cancellation, reload SHALL detach all remaining steps from caller cancellation and deadlines before any optional build-branch checkout or pull. Git commands SHALL use Iris's configured git transfer timeout; hooks, build, and restart SHALL retain their separate configured timeouts. Genuine failures SHALL remain terminal failures. Branch restoration and self-exit scheduling SHALL remain effective after caller cancellation.

#### Scenario: Caller cancels during reload build

- **WHEN** a caller cancels during a reload build
- **THEN** the successful build is followed by restart dispatch and any configured verification, and the result is audited

### Requirement: Reload persists explicit restart outcome

Terminal reload audit entries SHALL include `restart_attempted` and `restart_succeeded`, describing restart dispatch rather than verified service health. Pre-restart failures SHALL leave both false, restart failures SHALL set only attempted true, and verification failures SHALL retain both true. Self-reload SHALL preserve `restart_pending = true` until its existing scheduled-exit choreography.

#### Scenario: Verification fails after a successful restart

- **WHEN** restart dispatch succeeds but verification fails
- **THEN** the audit reports failure with both restart flags true and the verification failure reason
