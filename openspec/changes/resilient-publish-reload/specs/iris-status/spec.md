## ADDED Requirements

### Requirement: Status exposes persisted restart outcome

`iris_status.last_reload` SHALL expose the latest terminal audit entry's additive `restart_attempted` and `restart_succeeded` flags alongside caller, timestamp, applied SHA, outcome, and failure reason. Historical entries without these fields SHALL decode as false; their absence in the original log is not proof that restart was skipped. While an operation runs, status MAY still show an earlier terminal entry.

#### Scenario: Caller checks a publish after losing its response

- **WHEN** a caller whose wait timed out reads status after publish completes
- **THEN** the latest matching publish audit record shows its SHA, caller, timestamp, restart dispatch outcome, and any failure reason
