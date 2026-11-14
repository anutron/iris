## ADDED Requirements

### Requirement: Absolute hold deadline

Iris SHALL read positive integer `max_hold_seconds` from the local-kind `[dogfood_lease]` block, default 1800, validate it through iris_validate_config, and expire a hold at granted_at plus that value regardless of waiters, busy state, Argus reachability or renewal. At the deadline a fresh claim SHALL join behind existing waiters; with an empty queue it can receive a new grant. Contact, deployment and restart SHALL NOT extend that deadline. A valid in-flight mark SHALL defer automatic release until completion or its existing configured cap. Expiry SHALL be evaluated before claim, renewal and gate entry, on ticks and after EndFlight.

#### Scenario: Renewals cannot monopolize the host

- **GIVEN** a busy holder renews repeatedly with no waiters
- **WHEN** its original grant reaches max_hold_seconds
- **THEN** the lease is released and renewal is refused

#### Scenario: Flight defers expiry for a bounded interval

- **GIVEN** a deployment is in flight at the absolute deadline
- **WHEN** it finishes or its in-flight mark expires
- **THEN** Iris releases the holder and grants the next live waiter

#### Scenario: Invalid absolute hold config

- **WHEN** max_hold_seconds is zero or negative
- **THEN** iris_validate_config reports dogfood_lease.max_hold_seconds as invalid

### Requirement: Lease deadlines and reservation are visible

Claim and renewal results and iris_status SHALL expose holder identity, grant time, last Iris contact and absolute expires_at with plain release instructions. Claim and status SHALL expose queue positions and estimated handoff times based on the maximum hold and in-flight allowance under current configuration and ordering. Scheduler and API delays can add time. These SHALL be labelled estimates that can change with configuration or queue reordering. Grant notices SHALL state expiry and that renewal cannot extend it. Successful deployment guidance SHALL name iris_status for expiry. Iris SHALL best-effort stamp compact per-repo lease summaries in Argus task metadata for holders and waiters on changes, and clear departed participants' summaries. Metadata failures SHALL NOT fail lease operations.

The skill and snippet SHALL instruct coordinators to call iris_dogfood_claim with position 1 and no SHA to reserve the next merge window, then release after composing and deployment/testing. This SHALL use existing ordering rules, SHALL NOT preempt a holder, and SHALL NOT be satisfied by commit coalescing.

#### Scenario: Coordinator reserves next slot

- **GIVEN** A holds the lease and B and C wait
- **WHEN** coordinator D claims position 1 without a SHA and A releases
- **THEN** D is granted before B and C, even if deployed commits are contained

#### Scenario: Agent sees hold limit

- **WHEN** a holder claims, renews or reads status
- **THEN** its result includes the original deadline and instructions to release when done

#### Scenario: Metadata is advisory

- **WHEN** the Argus metadata endpoint rejects a write
- **THEN** the lease operation succeeds and returns a warning

## MODIFIED Requirements

### Requirement: Liveness consulted before releasing on silence

On every scheduler tick, before any release on silence (final warning, scheduled release), when a check-in falls due, when a claim arrives against a held lease, and before granting to a queued task, iris SHALL check the task's real state with argus (`GET /api/tasks/{id}` and `GET /api/tasks/{id}/bridge`) and classify it:

- **Dead:** the task returns 404, has `status = "complete"`, has `archived = true`, explicitly reports task `live = false`, or its bridge reports `live = false`. A dead holder SHALL be released immediately, with no grace and no final warning. A dead queued task SHALL be dropped from the queue and skipped.
- **Busy:** the bridge reports `live = true` and `busy = true`, with `needs_input = false`. The holder SHALL be treated as alive, and iris SHALL NOT release on silence. The idle-release cap still applies.
- **Silent:** the bridge reports `needs_input = true` even when busy, or the bridge is not busy and there has been no contact. Normal silence rules apply.
- **Unknown, no bridge:** the bridge endpoint returns 404 (for example a non-Claude backend). Iris SHALL apply the plain silence rules, using task status and explicit task live state for the dead check.
- **Unknown, argus unreachable:** argus is unreachable or returns a 5xx error. Iris SHALL NOT release on silence and SHALL retry on its next evaluation. The idle-release cap still applies.

#### Scenario: Archived holder is released without grace

- **GIVEN** `A` holds the lease and `B` is queued
- **WHEN** the next tick runs and argus reports `A` as `archived = true`
- **THEN** iris releases `A` immediately, with no check-in and no "releasing in 30s" warning, and grants the lease to `B`

#### Scenario: Busy holder is not released on silence

- **GIVEN** `A` holds the lease, `B` is queued, and `A` has not answered a check-in for 30 seconds
- **WHEN** the bridge reports `A` as `live = true, busy = true`
- **THEN** iris sends no final warning and does not release `A`

#### Scenario: Holder stuck on a prompt counts as silent

- **GIVEN** `A` holds the lease, `B` is queued, and `A` has not answered a check-in for 30 seconds
- **WHEN** the bridge reports `A` as `live = true, busy = true, needs_input = true`
- **THEN** iris sends the final warning and, if `A` is still silent after `grace_seconds`, releases `A` to `B`

#### Scenario: Holder without a bridge falls back to plain silence rules

- **GIVEN** `A` runs on a backend for which `GET /api/tasks/A/bridge` returns 404, `A`'s task status is `in_progress`, and `B` is queued
- **WHEN** `A` ignores a check-in and the final warning
- **THEN** iris releases `A` to `B` on the plain silence timeline

#### Scenario: Argus unreachable does not release on silence

- **GIVEN** `A` holds the lease, `B` is queued, and argus is unreachable
- **WHEN** a check-in's reply window passes
- **THEN** iris does not release `A`, retries on its next evaluation, and `iris_status` shows the liveness lookup error

#### Scenario: Dead queued task is skipped at grant time

- **GIVEN** `A` holds the lease and `B`, then `C`, are queued, and argus reports `B`'s task as `complete`
- **WHEN** `A` releases
- **THEN** iris drops `B` from the queue and grants the lease to `C`

#### Scenario: Stopped non-Claude task releases without bridge

- **WHEN** the task endpoint reports live false even though no bridge exists
- **THEN** Iris releases its lease on the next tick
