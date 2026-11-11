## ADDED Requirements

### Requirement: `iris:status` reports the dogfood lease

The `iris:status` verb SHALL include a `dogfood_lease` field in its result when the source repo's merged configuration declares `dogfood_branch`. When it does not, the field SHALL be `null`. Reading the lease SHALL have no side effects: no messages, no timer changes, and no liveness-driven releases.

The field shape:

- `holder`: `null`, or `{ task_id, task_name, granted_at, last_contact_at, next_checkin_at, checkin_outstanding_since?, release_at?, in_flight_set_dogfood: bool }`.
- `queue`: an ordered array of `{ task_id, task_name, position, enqueued_at }`.
- `timers`: `{ warmer_seconds, grace_seconds, idle_release_seconds, idle_release_at? }`. These are the effective values after defaults and overlay. `idle_release_at` is set when there is a holder.
- `history`: the last 20 lease transitions, each `{ at, event, task_id, reason? }`.
- `warnings`: lease-specific problems, such as an unreadable lease file, invalid timer config, or the last liveness lookup or message delivery error.

A malformed `dogfood-lease.json` SHALL NOT fail the verb. Iris SHALL report `holder: null`, `queue: []` and a warning naming the file.

#### Scenario: Status shows holder, queue, and timers

- **GIVEN** task `A` holds the lease and task `B` is queued
- **WHEN** `iris:status` is invoked
- **THEN** `dogfood_lease.holder.task_id` is `A` with its `next_checkin_at`, `dogfood_lease.queue` is `[{ task_id: "B", position: 1, ... }]`, and `dogfood_lease.timers` shows the effective timer values

#### Scenario: Free lease reports a null holder

- **GIVEN** a source repo with `dogfood_branch` configured and no lease holder
- **WHEN** `iris:status` is invoked
- **THEN** `dogfood_lease` is `{ holder: null, queue: [], timers: {...}, history: [...] }`

#### Scenario: No dogfood branch reports null

- **GIVEN** a source repo whose merged config declares no `dogfood_branch`
- **WHEN** `iris:status` is invoked
- **THEN** `dogfood_lease` is `null` and no lease warning is added

#### Scenario: Status does not drive lease timers

- **GIVEN** a lease holder whose check-in is overdue
- **WHEN** `iris:status` is invoked
- **THEN** iris sends no message, releases no one, and does not change `dogfood-lease.json`
