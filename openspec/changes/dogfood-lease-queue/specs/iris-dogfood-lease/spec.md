## ADDED Requirements

### Requirement: Per-source-repo dogfood lease with an ordered queue

Iris SHALL keep one dogfood lease for each source repo whose merged configuration declares `dogfood_branch`. At most one argus task SHALL hold the lease at a time. Tasks waiting for it SHALL be kept in an ordered queue. The queue is first-in-first-out by default, and can be reordered by cut-in claims and by `iris:dogfood_queue_move`. The lease SHALL always be granted to the task at position 1. Lease identities (holder and queue entries) SHALL be argus task IDs. Every lease verb SHALL resolve its source repo the same way `iris:set_dogfood` does (`task_id` → source repo), so all tasks of one argus project share one lease. Iris SHALL keep this invariant: whenever the queue is non-empty, the lease has a holder. A free lease is granted immediately and never leaves anyone queued.

#### Scenario: Two tasks in the same project share one lease

- **GIVEN** argus tasks `A` and `B` whose worktrees resolve to the same source repo with `dogfood_branch = "dev"`
- **WHEN** `A` claims the lease and then `B` claims it
- **THEN** `A` is the holder and `B` is queued at position 1 for the same lease

#### Scenario: Lease verbs refuse a repo without dogfood_branch

- **GIVEN** a source repo whose merged config declares no `dogfood_branch`
- **WHEN** any lease verb is invoked against it
- **THEN** iris returns the same `dogfood_branch not configured` error `iris:set_dogfood` returns, and writes no lease state

### Requirement: `iris:dogfood_claim` verb

The plugin SHALL expose `iris:dogfood_claim` as an MCP tool (`iris_dogfood_claim`) and CLI subcommand (`iris dogfood claim`).

Inputs:

- `task_id` (string, required): the claiming argus task.
- `wait_seconds` (integer, optional, default 0, clamped to 0–25): how long to wait for a grant before returning.
- `position` (integer, optional, 1-based): cut in line. When omitted, a new claimant is appended to the end of the queue. When given, the claimant is placed at that position, clamped to `[1, queue_length + 1]` (counting the queue without the caller), and every task at or after that position moves back one. An already-queued caller that passes `position` moves itself. Cutting in line SHALL be recorded in the lease history. It SHALL NOT affect the holder.

Behavior:

- If the lease is free, iris SHALL grant it to the caller, record the grant as contact, and message the grant to the caller.
- If the caller already holds the lease, iris SHALL record contact and return `granted`.
- If the caller is already queued, iris SHALL return its current position without re-queuing it (moving it first if it passed `position`) and without triggering a new check-in.
- Otherwise iris SHALL first apply the liveness rules to the current holder. A dead holder is released, and the claim is re-evaluated against the freed lease. If the holder is not dead, iris SHALL insert the caller into the queue (at the end, or at its requested `position`) and immediately send the holder a check-in, unless a check-in is already outstanding.
- While the caller is queued and `wait_seconds` > 0, iris SHALL wait up to `wait_seconds` for the caller to be granted before responding. A later `claim` call by the same task acts as the poll.

Result shape:

- `status`: `"granted"` or `"queued"`.
- `dogfood_branch`: the configured dogfood branch.
- `holder`: `{ task_id, task_name }` of the current holder, where `task_name` is the holder's argus task name.
- `position`: 1-based queue position, or 0 when granted.
- `ahead`: an ordered array of `{ task_id, task_name, position }` for every queued task in front of the caller. It is empty when granted or at position 1.
- `queue_length`: the number of queued tasks.
- `next_checkin_at`: when granted, the time the warmer check-in falls due.
- `message`: a human-readable line. For `queued` it SHALL say the lease is busy, name the holder by argus task name, give the position, and say iris will message the caller when the lease is granted.

#### Scenario: Free lease is granted immediately

- **GIVEN** a source repo with `dogfood_branch = "dev"` and no lease holder
- **WHEN** task `A` calls `iris_dogfood_claim(task_id="A")`
- **THEN** iris returns `status: "granted"`, `position: 0`, and a `next_checkin_at` one warmer interval from now
- **AND** iris sends `A` a grant message naming the repo, the branch, the timer, and `iris_dogfood_release`

#### Scenario: Busy lease queues the caller and checks in on the holder

- **GIVEN** task `A` holds the lease and no check-in is outstanding
- **WHEN** task `B` calls `iris_dogfood_claim(task_id="B")`
- **THEN** iris returns `status: "queued"`, `position: 1`, `holder: { task_id: "A", task_name: "<A's argus task name>" }`, `ahead: []`, and a message that it will notify `B` when the lease frees
- **AND** iris immediately sends `A` a check-in that says one agent is waiting

#### Scenario: Queued caller sees everyone ahead of it

- **GIVEN** `A` holds the lease and `B`, then `C`, are queued
- **WHEN** task `D` calls `iris_dogfood_claim(task_id="D")`
- **THEN** iris returns `position: 3`, `holder` with `A`'s task ID and argus task name, and `ahead: [{ task_id: "B", task_name, position: 1 }, { task_id: "C", task_name, position: 2 }]`

#### Scenario: Agent cuts in line

- **GIVEN** `A` holds the lease and `B`, then `C`, are queued
- **WHEN** task `D` calls `iris_dogfood_claim(task_id="D", position=1)`
- **THEN** iris returns `status: "queued"`, `position: 1`, `ahead: []`; the queue is now `D`, `B`, `C`; `A` still holds the lease; and the lease history records that `D` cut in at position 1

#### Scenario: Cut-in position past the end is clamped

- **GIVEN** `A` holds the lease and `B` is queued
- **WHEN** task `C` calls `iris_dogfood_claim(task_id="C", position=9)`
- **THEN** `C` is placed at position 2, the end of the queue

#### Scenario: Re-claim while queued is a poll

- **GIVEN** `A` holds the lease and `B` is queued at position 1 with a check-in already outstanding on `A`
- **WHEN** `B` calls `iris_dogfood_claim` again
- **THEN** iris returns `status: "queued"`, `position: 1`, and does NOT send `A` another check-in

#### Scenario: Claim with wait is granted when the holder releases

- **GIVEN** `A` holds the lease
- **WHEN** `B` calls `iris_dogfood_claim(task_id="B", wait_seconds=20)` and `A` calls `iris_dogfood_release` 5 seconds later
- **THEN** `B`'s claim returns `status: "granted"` without waiting the full 20 seconds

#### Scenario: Claim against a dead holder is granted on the spot

- **GIVEN** `A` holds the lease and argus reports `A`'s task as `complete`
- **WHEN** `B` calls `iris_dogfood_claim(task_id="B")`
- **THEN** iris releases `A` without a grace period, grants the lease to `B`, and returns `status: "granted"`

#### Scenario: wait_seconds above the cap is clamped

- **WHEN** a queued caller passes `wait_seconds=120`
- **THEN** iris waits at most 25 seconds before returning `status: "queued"`

### Requirement: `iris:dogfood_queue_move` verb

The plugin SHALL expose `iris:dogfood_queue_move` as an MCP tool (`iris_dogfood_queue_move`) and CLI subcommand (`iris dogfood queue-move <task> <position>`). It lets a human or coordinator reorder the queue.

Inputs:

- `task_id` or `path` (optional): resolved like `iris:status`, to identify the source repo.
- `move_task_id` (string, required): the queued task to move.
- `position` (integer, required, 1-based): its new position, clamped to `[1, queue_length]`.

Iris SHALL move the task, shift the others to keep their relative order, record the move in the lease history, and return the new queue as an ordered array of `{ task_id, task_name, position }`. Iris SHALL NOT message tasks whose position changed; they see the new position on their next claim. A `move_task_id` that is not queued (including the holder) SHALL be refused with an error, and no state SHALL change.

#### Scenario: Human moves a task to the front

- **GIVEN** `A` holds the lease and `B`, `C`, `D` are queued in that order
- **WHEN** a human runs `iris dogfood queue-move D 1`
- **THEN** the queue becomes `D`, `B`, `C`, `A` still holds the lease, and the move appears in the lease history

#### Scenario: Moving the holder is refused

- **GIVEN** `A` holds the lease and `B` is queued
- **WHEN** `iris_dogfood_queue_move(move_task_id="A", position=1)` is invoked
- **THEN** iris returns an error saying `A` holds the lease and is not in the queue, and changes no state

### Requirement: `iris:dogfood_renew` verb

The plugin SHALL expose `iris:dogfood_renew` as an MCP tool (`iris_dogfood_renew`) and CLI subcommand (`iris dogfood renew`) that accepts `task_id` (string, required). It is the holder's "still using it" reply. When the caller holds the lease, iris SHALL record contact and return `{ renewed: true, next_checkin_at }`. Recording contact restarts the warmer clock, clears any outstanding check-in, final warning and scheduled release, and cancels any undelivered iris messages to the holder. When the caller does not hold the lease, iris SHALL return an error that names the current holder (task ID and argus task name), gives the caller's queue position if it has one, and lists the queued tasks ahead of it.

#### Scenario: Renew restarts the timer

- **GIVEN** `A` holds the lease and iris sent `A` a check-in 10 seconds ago
- **WHEN** `A` calls `iris_dogfood_renew(task_id="A")`
- **THEN** iris returns `renewed: true` with `next_checkin_at` one warmer interval from now, and the outstanding check-in is cleared

#### Scenario: Renew cancels a scheduled release

- **GIVEN** `A` holds the lease, `B` is queued, and iris has sent `A` the final "releasing in 30s" warning
- **WHEN** `A` calls `iris_dogfood_renew` before the grace period ends
- **THEN** the scheduled release is cancelled, `A` keeps the lease, and `B` stays queued

#### Scenario: Renew by a non-holder is refused

- **GIVEN** `A` holds the lease and `B` is queued at position 1
- **WHEN** `B` calls `iris_dogfood_renew(task_id="B")`
- **THEN** iris returns an error naming `A` as the holder and `B`'s position 1, and the lease state is unchanged

### Requirement: `iris:dogfood_release` verb

The plugin SHALL expose `iris:dogfood_release` as an MCP tool (`iris_dogfood_release`) and CLI subcommand (`iris dogfood release`) that accepts `task_id` (string, required).

- When the caller holds the lease, iris SHALL release it and grant it to the next live queued task, if any.
- When the caller is queued, iris SHALL remove it from the queue.
- When the caller is neither, the call SHALL succeed as a no-op.

The result SHALL be `{ released: bool, was: "holder" | "queued" | "none", granted_to?: string }`.

#### Scenario: Holder releases and the next in line is granted

- **GIVEN** `A` holds the lease and `B` is queued at position 1
- **WHEN** `A` calls `iris_dogfood_release(task_id="A")`
- **THEN** iris returns `{ released: true, was: "holder", granted_to: "B" }`, makes `B` the holder, and sends `B` a grant message saying the lease is theirs and naming the timer

#### Scenario: Queued task leaves the queue

- **GIVEN** `A` holds the lease and `B` and `C` are queued in that order
- **WHEN** `B` calls `iris_dogfood_release(task_id="B")`
- **THEN** iris returns `{ released: true, was: "queued" }` and `C` moves to position 1

#### Scenario: Release when not involved is a no-op

- **WHEN** a task that neither holds the lease nor is queued calls `iris_dogfood_release`
- **THEN** iris returns `{ released: false, was: "none" }` and changes no state

### Requirement: `iris:dogfood_force_release` verb

The plugin SHALL expose `iris:dogfood_force_release` as an MCP tool (`iris_dogfood_force_release`) and CLI subcommand (`iris dogfood force-release`). It is the human or coordinator override for a stuck holder.

Inputs:

- `task_id` or `path` (optional): resolved like `iris:status`, to identify the source repo.
- `reason` (string, required).

Iris SHALL release the current holder whatever its liveness or in-flight state, record the reason in the lease history, message the evicted holder with the reason, and grant the lease to the next live queued task. Force-release SHALL succeed even when argus is unreachable; messaging is then skipped and a warning is returned. With no holder, it SHALL return `{ released: false }`.

#### Scenario: Force-release evicts a stuck holder

- **GIVEN** `A` holds the lease, the bridge reports `A` as `busy=true`, and `B` is queued
- **WHEN** a human runs `iris dogfood force-release --reason "A is looping"`
- **THEN** iris releases `A`, messages `A` with the reason, grants the lease to `B`, and records the reason in the lease history

#### Scenario: Force-release without a reason is refused

- **WHEN** `iris_dogfood_force_release` is invoked with no `reason`
- **THEN** iris returns an error and changes no state

### Requirement: Warmer check-ins and contact

Iris SHALL treat these holder actions as contact:

- an `iris_dogfood_renew` call
- an `iris_dogfood_claim` call
- the start and the end of an `iris_set_dogfood` call

Granting the lease SHALL also count as contact. When `warmer_seconds` have passed since the holder's last contact and no check-in is outstanding, iris SHALL apply the liveness rules and, unless the holder is released as dead, send the holder a check-in. The check-in SHALL ask whether the holder is still using dogfood and name `iris_dogfood_renew` (yes) and `iris_dogfood_release` (done). When someone is queued, the check-in SHALL say how many are waiting. A new task joining the queue SHALL trigger an immediate check-in on the holder, unless one is already outstanding.

#### Scenario: Check-in sent after the warmer interval

- **GIVEN** `A` was granted the lease with the default `warmer_seconds = 600` and has made no contact since
- **WHEN** 600 seconds pass
- **THEN** iris sends `A` a check-in naming `iris_dogfood_renew` and `iris_dogfood_release`

#### Scenario: set_dogfood by the holder counts as contact

- **GIVEN** `A` holds the lease and its warmer check-in is due in 30 seconds
- **WHEN** `A` calls `iris_set_dogfood`
- **THEN** the warmer clock restarts from that call and no check-in is sent at the original deadline

### Requirement: Release on silence only when someone is waiting

When the queue is non-empty and the holder has made no contact for `grace_seconds` after a delivered check-in, iris SHALL apply the liveness rules:

- If the holder is dead, iris SHALL release it immediately.
- If the holder is busy, iris SHALL defer.
- If the holder is silent, iris SHALL send a final warning saying the lease will be released in `grace_seconds` unless it calls `iris_dogfood_renew`, and schedule the release `grace_seconds` later.

When the scheduled release arrives, iris SHALL apply the liveness rules again. If the holder is still silent, iris SHALL release it, message it that it was released for silence, and grant the lease to the next live queued task with a grant message.

When the queue is empty, a missed check-in SHALL NOT release the lease, and no final warning SHALL be sent. The next check-in SHALL be sent `warmer_seconds` after the missed one.

#### Scenario: Aaron's script — silence with a waiter hands the lease over

- **GIVEN** `A` holds the lease, `B` is queued, and both have live bridges
- **WHEN** iris sends `A` a check-in, `A` stays silent (`busy=false`, `needs_input=false`) for 30 seconds, iris sends "releasing in 30s", and `A` stays silent another 30 seconds
- **THEN** iris releases `A`, messages `A` that the lease was released, makes `B` the holder, and messages `B` that dogfood is theirs with a 10-minute timer

#### Scenario: Silence with nobody waiting does not release

- **GIVEN** `A` holds the lease and the queue is empty
- **WHEN** `A` ignores a check-in for longer than `grace_seconds`
- **THEN** `A` keeps the lease, no "releasing" warning is sent, and the next check-in is sent `warmer_seconds` after the missed one

#### Scenario: Waiter joins while a check-in is already overdue

- **GIVEN** `A` holds the lease, the queue is empty, and `A` ignored a check-in sent 5 minutes ago
- **WHEN** `B` claims the lease
- **THEN** iris applies the liveness rules to `A` and, if `A` is silent, sends the final warning right away instead of sending a new check-in

### Requirement: Idle release cap

When `idle_release_seconds` have passed since the holder's last contact, iris SHALL release the lease whether or not anyone is queued and whatever the holder's bridge state (including `busy=true`). Iris SHALL message the holder that the lease was released after the idle window, and grant the lease to the next live queued task, if any. The only thing that defers the idle release is an `iris_set_dogfood` call by the holder that is still running.

#### Scenario: Holder idle for 30 minutes with nobody waiting

- **GIVEN** `A` holds the lease, the queue is empty, and `A` has made no contact for 1800 seconds (default `idle_release_seconds`)
- **THEN** iris releases the lease, messages `A` that it was released after 30 minutes without word, and the lease becomes free

#### Scenario: Busy holder is capped when someone is waiting

- **GIVEN** `A` holds the lease, `B` is queued, and the bridge has reported `A` as `busy=true` at every check since `A`'s last contact 1800 seconds ago
- **THEN** iris releases `A` and grants the lease to `B`

### Requirement: Liveness consulted before releasing on silence

Before any release on silence (final warning, scheduled release), when a check-in falls due, when a claim arrives against a held lease, and before granting to a queued task, iris SHALL check the task's real state with argus (`GET /api/tasks/{id}` and `GET /api/tasks/{id}/bridge`) and classify it:

- **Dead:** the task returns 404, has `status = "complete"`, has `archived = true`, or its bridge reports `live = false`. A dead holder SHALL be released immediately, with no grace and no final warning. A dead queued task SHALL be dropped from the queue and skipped.
- **Busy:** the bridge reports `live = true` and `busy = true`. The holder SHALL be treated as alive, and iris SHALL NOT release on silence. The idle-release cap still applies.
- **Silent:** the bridge reports `needs_input = true` and is not busy, or the bridge is not busy and there has been no contact. Normal silence rules apply.
- **Unknown, no bridge:** the bridge endpoint returns 404 (for example a non-Claude backend). Iris SHALL apply the plain silence rules, using task status alone for the dead check.
- **Unknown, argus unreachable:** argus is unreachable or returns a 5xx error. Iris SHALL NOT release on silence and SHALL retry on its next evaluation. The idle-release cap still applies.

#### Scenario: Archived holder is released without grace

- **GIVEN** `A` holds the lease and `B` is queued
- **WHEN** a check-in falls due and argus reports `A` as `archived = true`
- **THEN** iris releases `A` immediately, with no check-in and no "releasing in 30s" warning, and grants the lease to `B`

#### Scenario: Busy holder is not released on silence

- **GIVEN** `A` holds the lease, `B` is queued, and `A` has not answered a check-in for 30 seconds
- **WHEN** the bridge reports `A` as `live = true, busy = true`
- **THEN** iris sends no final warning and does not release `A`

#### Scenario: Holder stuck on a prompt counts as silent

- **GIVEN** `A` holds the lease, `B` is queued, and `A` has not answered a check-in for 30 seconds
- **WHEN** the bridge reports `A` as `live = true, busy = false, needs_input = true`
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

### Requirement: Iris messages agents through argus pane delivery

Iris SHALL deliver every lease message to the recipient's argus session with `POST /api/tasks/{id}/notify`, using `submit: true` and a unique `delivery_id` (alphanumerics, `-`, `_`; ≤ 128 bytes).

The lease messages are:

- grant
- check-in
- final warning
- released (with the reason)
- lease override notice
- recompose-skipped notice

Every message SHALL be prefixed `[iris]` and SHALL name the source repo and the exact iris tool the recipient should call to respond. Agents respond by calling iris verbs, not by replying in chat.

Delivery deadlines:

- check-ins and final warnings SHALL use a delivery deadline of `warmer_seconds`
- all other messages SHALL use the argus maximum, one hour

When the holder makes contact, iris SHALL cancel its pending check-in and final-warning deliveries (`DELETE /api/tasks/{id}/notify/{delivery_id}`).

A check-in that iris failed to submit (any error other than 404) SHALL NOT start the silence countdown, and iris SHALL retry it on its next evaluation. A 404 from the notify endpoint SHALL be treated as the dead liveness state.

#### Scenario: Grant message names the release tool

- **WHEN** iris grants the lease to task `B`
- **THEN** iris posts to `/api/tasks/B/notify` text beginning `[iris]` that names the repo, the dogfood branch, the warmer timer, and `iris_dogfood_release`

#### Scenario: Contact cancels a pending check-in delivery

- **GIVEN** iris posted a check-in to busy holder `A` that argus has not yet submitted
- **WHEN** `A` calls `iris_set_dogfood`
- **THEN** iris cancels that delivery, so `A` never receives a stale "still using it?" prompt

#### Scenario: Undelivered check-in does not count as silence

- **GIVEN** `A` holds the lease and `B` is queued
- **WHEN** iris's check-in post to argus fails with a 503
- **THEN** no silence countdown starts, and iris retries the check-in on its next evaluation

### Requirement: Lease state persists across iris restarts

Iris SHALL persist each lease as `dogfood-lease.json` in the same per-source-repo state directory as `dogfood-manifest.json`. The file holds:

- the holder: task ID, task name, granted time, last contact, outstanding check-in, final warning and scheduled release times, and pending delivery IDs
- the ordered queue
- the last 20 lease transitions as history

Iris SHALL write the file atomically (write a temp file, then rename) on every state change. On startup iris SHALL load every persisted lease and resume its timers. An iris restart SHALL NOT count as holder silence: any outstanding check-in's reply window and any scheduled release SHALL be restarted from the startup time. The holder's last-contact time SHALL be kept unchanged, so the idle-release cap still holds across restarts.

#### Scenario: Lease survives an iris restart

- **GIVEN** `A` holds the lease and `B` is queued
- **WHEN** iris restarts
- **THEN** `iris_status` reports `A` as holder and `B` at position 1, and the warmer check-in falls due at the same time it would have without the restart

#### Scenario: Restart does not trigger an immediate release

- **GIVEN** `A` holds the lease, `B` is queued, and iris scheduled `A`'s release for 10 seconds from now
- **WHEN** iris is down for 2 minutes and then starts
- **THEN** iris does not release `A` at startup; the release is rescheduled `grace_seconds` after startup, and is cancelled if `A` makes contact first

### Requirement: Configurable lease timers

Iris SHALL read lease timers from an optional `[dogfood_lease]` block of the merged `.iris.toml` overlay at the source repo root. When `.iris.toml` is absent (the bootstrap case), iris SHALL read the block directly from `.iris.local.toml`, the same way `dogfood_branch` is resolved. The keys and defaults are:

- `warmer_seconds`, default 600
- `grace_seconds`, default 30
- `idle_release_seconds`, default 1800

Iris SHALL use the defaults for a missing block or a missing key. Iris SHALL re-read the configuration whenever it evaluates a lease, so edits take effect without a restart. If the configuration is invalid, iris SHALL fall back to the defaults and report a warning in `iris_status`.

#### Scenario: Defaults apply without a block

- **GIVEN** a source repo whose merged config declares `dogfood_branch` but no `[dogfood_lease]` block
- **WHEN** a task claims the lease
- **THEN** the warmer is 600 seconds, the grace is 30 seconds, and the idle release is 1800 seconds

#### Scenario: Configured timers override the defaults

- **GIVEN** `.iris.local.toml` declares `[dogfood_lease] warmer_seconds = 300`
- **WHEN** a task is granted the lease
- **THEN** its first check-in falls due 300 seconds after the grant
