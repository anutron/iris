## Why

Caller deadlines currently kill publish/reload builds after the host checkout has changed, leaving an old server serving stale code. The October 15 Sketch incident exposed this cancellation gap.

## What changes

- Continue the locked mutation, build, restart, and verification sequence independently of caller cancellation, retaining Iris-owned step timeouts.
- Record whether restart dispatch was attempted and succeeded in the existing audit log, available through `iris_status.last_reload`.
- Cover cancelled callers, actual build/restart failures, and configured timeouts with fixture-only tests.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `iris-publish`: Complete the mutation sequence despite caller cancellation and persist restart outcome.
- `iris-reload`: Apply the same cancellation boundary, including branch restoration and verification.
- `iris-status`: Expose explicit restart outcome in the existing latest audit record.

## Impact

Changes are limited to verb context ownership, additive audit fields, tests, and documentation. No new API, job scheduler, or dependencies; no running host services are changed during validation.
