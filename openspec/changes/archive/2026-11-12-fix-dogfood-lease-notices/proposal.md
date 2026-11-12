## Why

Live use of the dogfood lease (PR #22) exposed three defects. Iris granted the lease to a task, the task released it three seconds later, and the "dogfood is yours" pane notice was still typed into the task's session afterwards, because argus holds a pane delivery until the recipient is idle. Iris only ever tracked check-in and final-warning delivery IDs, so grant, released and override notices could never be cancelled. A check-in was recorded as sent but never visibly arrived, and iris threw away the state argus returned. And a killed `iris set-dogfood` can leave the holder's in-flight mark set forever, which exempts the holder from every silence, dead and idle release.

## What Changes

- Iris tracks the delivery ID of every lease notice in the persisted lease state and cancels any still-pending notice as soon as lease state makes it obsolete (holder releases, is released or force-released, renews, claims again, or a grant is superseded).
- Every notice carries a lease generation and a timestamp, and the iris skill tells agents to confirm with `iris_status` or the claim result before acting on a notice.
- Iris records the delivery state argus returns for each notice (`pending` vs `submitted`) and surfaces an unsubmitted check-in in `iris_status` warnings.
- An in-flight mark expires after `[dogfood_lease] in_flight_max_seconds` (default 900).
- Tests for the `iris dogfood *` CLI wiring and `--lease-override`.

## Capabilities

### New Capabilities

### Modified Capabilities
- `iris-dogfood-lease`: notice cancellation, self-describing notices, delivery-state reporting, in-flight expiry.

## Impact

`internal/verbs/dogfood_lease_state.go`, `internal/verbs/dogfood_lease.go`, `internal/argus/notify.go`, `internal/config/dogfood_lease.go`, `claude/skills/iris/SKILL.md`, `cmd/iris` tests.
