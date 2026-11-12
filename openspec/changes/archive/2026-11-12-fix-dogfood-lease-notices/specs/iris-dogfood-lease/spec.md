## ADDED Requirements

### Requirement: Obsolete lease notices are cancelled

Iris SHALL persist the delivery ID of every lease notice it posts (grant, check-in, final warning, released, override, recompose-skipped) in the lease state, with the recipient task and an expiry equal to the delivery deadline, so cancellation survives an iris restart. Iris SHALL cancel (`DELETE /api/tasks/{id}/notify/{delivery_id}`) every still-pending notice for a task whenever lease state makes it obsolete: the task releases, is released, is force-released or leaves the queue; the task makes contact (renew, claim, set_dogfood), which supersedes its grant, check-in and final-warning notices; or the task claims again, which supersedes its released and override notices. A cancel SHALL be issued before any new notice is posted in the same operation. Cancel failures SHALL NOT fail the operation. Persisted entries past their expiry SHALL be pruned without a cancel call.

#### Scenario: Release cancels a pending grant notice

- **GIVEN** iris granted the lease to `A` and argus has not yet submitted the grant notice
- **WHEN** `A` calls `iris_dogfood_release`
- **THEN** iris cancels the grant notice, so `A` never sees "dogfood is yours" after releasing

#### Scenario: Force release cancels the evicted holder's pending notices

- **GIVEN** `A` holds the lease with a pending check-in
- **WHEN** the lease is force-released
- **THEN** iris cancels `A`'s pending check-in before posting the released notice to `A`

#### Scenario: Contact supersedes a pending grant notice

- **GIVEN** a pending grant notice for holder `A`
- **WHEN** `A` calls `iris_dogfood_renew`
- **THEN** iris cancels the grant notice

#### Scenario: Cancellation survives a restart

- **GIVEN** a pending grant notice recorded in `dogfood-lease.json` and iris then restarts
- **WHEN** `A` releases the lease
- **THEN** iris still cancels the grant notice using the persisted delivery ID

#### Scenario: Re-claim supersedes a released notice

- **GIVEN** `A` was force-released and its released notice is still pending
- **WHEN** `A` calls `iris_dogfood_claim`
- **THEN** iris cancels the released notice

### Requirement: Lease notices are self-describing

Every lease notice SHALL state the lease generation (a counter incremented at each grant, persisted in the lease state) and the time it was issued, so a recipient can tell a stale notice from a current one. The iris agent skill SHALL tell agents to confirm with `iris_status` or the claim result before acting on a notice.

#### Scenario: Notice carries generation and time

- **WHEN** iris grants the lease for the second time in a repo
- **THEN** the grant notice contains `lease #2` and an RFC 3339 timestamp

### Requirement: Notice delivery state is recorded

Iris SHALL read the `state` argus returns from the notify endpoint (`pending` or `submitted`). When a check-in is accepted but not submitted, iris SHALL still start the check-in clock, and `iris_status` SHALL include a warning that the latest check-in is pending delivery until the holder makes contact or the check-in is dropped (cancelled or past its deadline). Contact SHALL NOT cancel override or recompose notices, which report something the holder has not yet seen.

#### Scenario: Pending check-in is visible

- **GIVEN** argus answers a check-in post with state `pending`
- **WHEN** `iris_status` is read
- **THEN** its lease warnings say the check-in to the holder has not been submitted yet

### Requirement: Stale in-flight marks expire

A holder's in-flight mark (set while its `iris_set_dogfood` call runs) SHALL record when it was set and SHALL be cleared once older than `in_flight_max_seconds` from the optional `[dogfood_lease]` block (default 900, positive integer, validated by `iris_validate_config`). Clearing it SHALL be recorded in the lease history, and the holder SHALL then be subject to ordinary silence, dead and idle releases. The expiry SHALL be evaluated on every lease operation and tick.

#### Scenario: Killed CLI deploy does not pin the lease

- **GIVEN** holder `A` has an in-flight mark older than `in_flight_max_seconds` and `B` is queued
- **WHEN** iris evaluates the lease
- **THEN** the mark is cleared, a history entry records it, and the normal release rules apply to `A`

#### Scenario: Invalid in_flight_max_seconds

- **WHEN** `[dogfood_lease] in_flight_max_seconds = 0`
- **THEN** `iris_validate_config` reports `dogfood_lease.in_flight_max_seconds must be a positive integer`
