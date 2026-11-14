# iris-dogfood-lease Specification

## Purpose
TBD - created by archiving change dogfood-lease-queue. Update Purpose after archive.
## Requirements
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
- `sha` (string, optional): the commit required on dogfood. Iris SHALL resolve commit references such as `main` in the host source repo to a full commit SHA before changing lease state; invalid or option-like values SHALL fail without side effects. The CLI SHALL expose `--sha`. An explicit SHA replaces an existing queued request; an omitted SHA retains a previously stored request, or preserves legacy task-only behavior when none exists.
- `wait_seconds` (integer, optional, default 0, clamped to 0–25): how long to wait for a grant before returning.
- `position` (integer, optional, 1-based): cut in line. When omitted, a new claimant is appended to the end of the queue. When given, the claimant is placed at that position, clamped to `[1, queue_length + 1]` (counting the queue without the caller), and every task at or after that position moves back one. An already-queued caller that passes `position` moves itself. Cutting in line SHALL be recorded in the lease history. It SHALL NOT affect the holder.

Behavior:

- If the caller already holds the lease, iris SHALL record contact and return `granted`, including when its requested commit is already contained. It SHALL NOT silently release an existing holder.
- Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing. It SHALL grant no lease, add no queue entry, remove the caller's existing queue entry if any, cancel obsolete notices, and leave the holder untouched. A missing dogfood branch SHALL count as not deployed; Git errors SHALL never count as containment. Removing the last waiter SHALL cancel the pending release-on-silence warning and schedule.
- If the lease is free, iris SHALL grant it to the caller, record the grant as contact, and message the grant to the caller.
- If the caller is already queued, iris SHALL return its current position without re-queuing it (moving it first if it passed `position`) and without triggering a new check-in.
- Otherwise iris SHALL first apply the liveness rules to the current holder. A dead holder is released, and the claim is re-evaluated against the freed lease. If the holder is not dead, iris SHALL insert the caller into the queue (at the end, or at its requested `position`) and immediately send the holder a check-in, unless a check-in is already outstanding.
- While the caller is queued and `wait_seconds` > 0, iris SHALL wait up to `wait_seconds` for the caller to be granted or its requested commit to become contained before responding; either result SHALL end the wait promptly. A later `claim` call by the same task acts as the poll.

Result shape:

- `status`: `"granted"`, `"queued"`, or `"already_deployed"`.
- `sha`: the full resolved requested commit when present, including a retained queued request.
- `dogfood_branch`: the configured dogfood branch.
- `holder`: `{ task_id, task_name }` of the current holder, where `task_name` is the holder's argus task name, or null when the lease is free.
- `position`: 1-based queue position, or 0 when granted or already deployed.
- `ahead`: an ordered array of `{ task_id, task_name, position }` for every queued task in front of the caller. It is empty when granted, already deployed, or at position 1.
- `queue_length`: the number of queued tasks.
- `next_checkin_at`: when granted, the time the warmer check-in falls due. It SHALL be absent for `already_deployed`.
- `message`: a human-readable line. For `queued` it SHALL say the lease is busy, name the holder by argus task name, give the position, and say iris will message the caller when the lease is granted or its commit is deployed.

For `already_deployed`, the message SHALL name the resolved commit and branch, say the commit is live and no lease is needed, and qualify that `--force` or external branch rewrites can remove it. Containment describes branch contents, not a new runtime-health guarantee. A successful containment check SHALL send no grant notice and SHALL NOT refresh the holder's contact clock.

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

#### Scenario: Busy lease does not queue an already deployed ancestor

- **GIVEN** `A` holds the lease and `dev` contains commit `S` as an ancestor
- **WHEN** `B` claims with `sha=S`
- **THEN** iris returns `already_deployed`, `sha=S`, position 0, empty ahead, no next-checkin time, and holder `A`
- **AND** iris does not queue `B`, message a grant, contact `A`, or send a check-in
- **AND** the message explains the forced-deploy and external-rewrite exceptions

#### Scenario: Free lease does not grant already deployed content

- **GIVEN** the lease is free and the dogfood HEAD equals commit `S`
- **WHEN** `B` claims with `sha=S`
- **THEN** iris returns `already_deployed` with holder null and leaves the lease free

#### Scenario: Commit absent from dogfood queues normally

- **GIVEN** `A` holds the lease and `dev` does not contain `S`
- **WHEN** `B` claims with `sha=S`
- **THEN** iris queues `B` normally and persists its full resolved requested SHA

#### Scenario: Ref is pinned when explicitly claimed

- **WHEN** `B` claims with `sha=main`, which resolves to `S` on the host
- **THEN** its request records `S`
- **AND** advancing `main` afterward does not change the queued request or the SHA checked by a waiting claim

#### Scenario: Reclaim retains or replaces a queued request

- **GIVEN** `B` is queued requiring `S`
- **WHEN** `B` claims without `sha`
- **THEN** its request remains `S`
- **WHEN** `B` claims explicitly with a different valid commit `T`
- **THEN** its request becomes `T` and containment is evaluated for `T`

#### Scenario: Poll returns promptly when requested content becomes available

- **GIVEN** `B` is queued requiring `S` with a waiting claim
- **WHEN** dogfood comes to contain `S`
- **THEN** the claim returns `already_deployed` without waiting out its timeout and without requeueing `B`

#### Scenario: Existing holder retains ownership

- **GIVEN** `A` holds the lease and dogfood contains `S`
- **WHEN** `A` claims with `sha=S`
- **THEN** iris returns `granted`, records contact, and keeps `A` as holder

#### Scenario: Invalid commit has no lease side effects

- **WHEN** a claim supplies an unresolvable commit or an option-like SHA beginning with `-`
- **THEN** iris returns an error without changing lease state, timers, or notices

#### Scenario: Missing branch cannot satisfy a request

- **GIVEN** the configured dogfood branch does not exist and the lease is free
- **WHEN** `B` claims with a valid commit `S`
- **THEN** iris grants the lease normally instead of returning `already_deployed`

### Requirement: `iris:dogfood_queue_move` verb

The plugin SHALL expose `iris:dogfood_queue_move` as an MCP tool (`iris_dogfood_queue_move`) and CLI subcommand (`iris dogfood queue-move <task> <position>`). It lets a human or coordinator reorder the queue.

Inputs:

- `task_id` or `path` (optional): resolved like `iris:status`, to identify the source repo.
- `move_task_id` (string, required): the queued task to move.
- `position` (integer, required, 1-based): its new position, clamped to `[1, queue_length]`.

Iris SHALL move the task, shift the others to keep their relative order, record the move in the lease history, and return `{ queue: [...] }`, the new queue as an ordered array of `{ task_id, task_name, position }`. A `position` below 1 is treated as 1. Iris SHALL NOT message tasks whose position changed; they see the new position on their next claim. A `move_task_id` that is not queued (including the holder) SHALL be refused with an error, and no state SHALL change.

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

Iris SHALL release the current holder whatever its liveness or in-flight state, record the reason in the lease history, message the evicted holder with the reason, and grant the lease to the next live queued task. Force-release SHALL succeed even when argus is unreachable; messaging is then skipped and a warning is returned. When argus cannot resolve the target, iris SHALL resolve the repo from the persisted lease state instead: the repo whose lease names the given `task_id` as holder or queued, or the given `path` when it has a lease. With no holder, it SHALL return `{ released: false }`.

#### Scenario: Force-release evicts a stuck holder

- **GIVEN** `A` holds the lease, the bridge reports `A` as `busy=true`, and `B` is queued
- **WHEN** a human runs `iris dogfood force-release --reason "A is looping"`
- **THEN** iris releases `A`, messages `A` with the reason, grants the lease to `B`, and records the reason in the lease history

#### Scenario: Force-release works with argus unreachable

- **GIVEN** `A` holds the lease, `B` is queued, and argus is unreachable
- **WHEN** a human runs `iris dogfood force-release --task B --reason "argus is down"`
- **THEN** iris resolves the repo from the persisted lease, releases `A`, grants the lease to `B`, skips messaging, and returns a warning

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
- deployed notice

Every message SHALL be prefixed `[iris]` and SHALL name the source repo and the exact iris tool the recipient should call to respond. Caller-influenced text in messages (repo, branch, reason, caller name, SHA) SHALL have control characters replaced and SHALL be truncated to 120 characters, and delivery deadlines SHALL NOT exceed one hour. Agents respond by calling iris verbs, not by replying in chat.

Delivery deadlines:

- check-ins and final warnings SHALL use a delivery deadline of `warmer_seconds`
- all other messages SHALL use the argus maximum, one hour

When the holder makes contact, iris SHALL cancel its pending check-in and final-warning deliveries (`DELETE /api/tasks/{id}/notify/{delivery_id}`).

A check-in that iris failed to submit (any error other than 404) SHALL NOT start the silence countdown, and iris SHALL retry it on its next evaluation. A 404 from the notify endpoint, for any lease message, SHALL be treated as the dead liveness state: if the recipient is the current holder it is released as dead and the lease passes to the next live queued task.

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

#### Scenario: Deployed notice explains satisfaction and its limits

- **WHEN** iris removes queued `B` because its requested commit is now contained
- **THEN** it posts to `/api/tasks/B/notify` with submit true, a unique delivery ID, a one-hour deadline, and sanitized text
- **AND** the notice begins `[iris]`, names the repo, commit, branch, lease generation and issue time, says the commit is now live and no lease is needed, and names `iris_status` for confirmation
- **AND** it explains that `--force` or an external branch rewrite can remove the commit

### Requirement: Lease state persists across iris restarts

Iris SHALL persist each lease as `dogfood-lease.json` in the same per-source-repo state directory as `dogfood-manifest.json`. The file holds:

- the holder: task ID, task name, granted time, last contact, outstanding check-in, final warning and scheduled release times, and pending delivery IDs
- the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior
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

#### Scenario: Requested commit survives restart

- **GIVEN** `B` is queued requiring full commit `S`
- **WHEN** iris restarts and a deployment contains `S`
- **THEN** iris can satisfy `B` using the persisted SHA without resolving the original ref again

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

### Requirement: Lease operations are serialized and evaluated by the daemon

Every lease read-modify-write SHALL be serialized per source repo across processes (the daemon and CLI commands) with an advisory file lock, and lease messages SHALL be sent while that lock is held so a contact cannot race a stale check-in onto the wire. The daemon SHALL evaluate every persisted lease that has a holder or a queue at least every 5 seconds, including leases claimed by a CLI process, and SHALL stop its scheduler on shutdown. A claim SHALL also evaluate the lease timers, and when that releases the holder the claim SHALL return the grant. A holder with a running `iris_set_dogfood` call is exempt from silence, dead and idle releases; a restart discards the in-flight mark. If the queue becomes empty before a scheduled release, iris SHALL cancel the release and the final warning.

#### Scenario: Last waiter leaving cancels the scheduled release

- **GIVEN** `A` holds the lease, `B` is queued, and iris has sent `A` the final "releasing in 30s" warning
- **WHEN** `B` calls `iris_dogfood_release` and the scheduled release time passes
- **THEN** `A` keeps the lease and iris sends no release message

#### Scenario: A claim made by a CLI process is ticked by the daemon

- **GIVEN** a lease claimed through `iris dogfood claim`
- **WHEN** the daemon's next evaluation runs
- **THEN** that lease's timers are evaluated like any other

#### Scenario: Notify 404 for a grant frees the lease

- **GIVEN** `A` holds the lease, `B` is queued, and argus answers 404 when iris posts a notify to `B`
- **WHEN** `A` calls `iris_dogfood_release`
- **THEN** `B` is released as dead and the lease becomes free

#### Scenario: Check-in deadlines never exceed one hour

- **GIVEN** `warmer_seconds = 7200`
- **WHEN** iris sends a check-in
- **THEN** its delivery deadline is one hour, the argus maximum

### Requirement: Obsolete lease notices are cancelled

Iris SHALL persist the delivery ID of every lease notice it posts (grant, check-in, final warning, released, override, recompose-skipped, deployed) in the lease state, with the recipient task, the requested SHA for a deployed notice, and an expiry equal to the delivery deadline, so cancellation survives an iris restart. Iris SHALL cancel (`DELETE /api/tasks/{id}/notify/{delivery_id}`) every still-pending notice for a task whenever lease state makes it obsolete: the task releases, is released, is force-released or leaves the queue; the task makes contact (renew, claim, set_dogfood), which supersedes its grant, check-in and final-warning notices; or the task claims again, which supersedes its released, override, recompose-skipped and deployed notices. A later release by a satisfied task SHALL cancel its pending deployed notice even though it is no longer queued. When an Iris dogfood branch move removes the SHA named by a still-pending deployed notice, iris SHALL cancel that notice; cancellation SHALL happen even when the subsequent reload fails. Contact by an unrelated holder SHALL NOT cancel the satisfied task's notice. A cancel SHALL be issued before any new notice is posted in the same operation. Cancel failures SHALL NOT fail the operation. Persisted entries past their expiry SHALL be pruned without a cancel call.

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

#### Scenario: Satisfaction cancels obsolete notices before posting

- **GIVEN** queued `B` requires `S` and has an older pending lease notice
- **WHEN** iris satisfies `B` after deployment
- **THEN** iris cancels the older notice before posting its deployed notice and persists the new notice's delivery ID and SHA

#### Scenario: Reclaim or release cancels deployment notice after restart

- **GIVEN** iris satisfied `B` and persisted its pending deployed notice, then restarted
- **WHEN** `B` calls claim or release
- **THEN** iris cancels that deployed notice, including when release is otherwise a no-op

#### Scenario: Forced rollback cancels pending satisfaction notice

- **GIVEN** a pending deployed notice tells `B` that `S` is live
- **WHEN** an Iris forced deploy moves dogfood to a commit that does not contain `S`
- **THEN** iris cancels the pending deployed notice even if reload later fails
- **AND** it does not automatically requeue `B`

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

### Requirement: Contained queued commits are satisfied without lease handoff

After a successful `iris_set_dogfood` including successful reload, Iris SHALL inspect the current configured dogfood branch and remove every queued task whose stored requested SHA is contained by its HEAD. It SHALL record each satisfaction in lease history, send each removed task a deployed notice through the existing Argus notice path, and preserve the holder and relative order of remaining waiters. This SHALL apply to ordinary, forced, override and internal deploys. A failed reload SHALL NOT cause success notices from that deploy. Queue reconciliation failures after a successful deploy SHALL return warnings and SHALL NOT falsely report the deploy itself as failed.

Before any queued task is granted the lease during a holder handoff, Iris SHALL also re-evaluate queued requested commits and satisfy contained entries instead of granting them. Release after deployment SHALL therefore be idempotent for satisfied tasks. Legacy queue entries without a SHA SHALL continue through the normal liveness and grant rules. Failed ancestry checks SHALL never satisfy a task; operational errors SHALL retain unproven entries and surface a warning. Removing the last waiter SHALL cancel release-on-silence scheduling and obsolete final warnings.

The Iris agent skill SHALL instruct callers needing a particular commit to pass `sha`, handle `already_deployed` without requesting deployment ownership, and confirm notices with `iris_status` or a new claim before acting. Existing holders SHALL still explicitly release when done.

#### Scenario: Holder deployment satisfies every matching waiter

- **GIVEN** `A` holds the lease and `B`, `C`, `D` are queued requiring `S`, unrelated `T`, and ancestor `R` of `S` respectively
- **WHEN** `A` successfully deploys a commit containing `S` and `R` but not `T`
- **THEN** iris removes `B` and `D`, notifies each that its commit is live, leaves `C` queued first, and keeps `A` as holder

#### Scenario: Legacy waiter remains queued

- **GIVEN** `A` holds the lease, `B` is queued without SHA, and `C` is queued requiring `S`
- **WHEN** a successful deployment contains `S`
- **THEN** only `C` is satisfied and `B` remains queued

#### Scenario: Release checks content before handoff

- **GIVEN** `A` holds the lease, `B` is queued requiring contained commit `S`, and `C` is queued requiring absent commit `T`
- **WHEN** `A` releases or is evicted
- **THEN** iris satisfies and notifies `B` and grants the lease to live task `C`

#### Scenario: Release after successful deployment sends no duplicate notice

- **GIVEN** `A` deployed `S` and iris already satisfied `B`
- **WHEN** `A` releases
- **THEN** iris neither grants the lease to `B` nor sends `B` a second deployed notice

#### Scenario: Failed deployment does not announce success

- **GIVEN** `B` is queued requiring `S`
- **WHEN** `set_dogfood` moves the branch to contain `S` but reload fails
- **THEN** that deploy does not remove `B` or post a success notice
- **AND** a later explicit claim or pre-handoff containment check still uses the observed branch contents

#### Scenario: Satisfying last waiter cancels silence eviction

- **GIVEN** `A` holds the lease, `B` is the last waiter requiring `S`, and a final warning scheduled `A`'s release
- **WHEN** iris finds `S` contained and removes `B`
- **THEN** iris cancels the pending final warning and scheduled silence release and keeps `A` as holder

#### Scenario: Operational Git failure cannot remove a waiter

- **GIVEN** `B` is queued requiring `S`
- **WHEN** checking branch containment fails with an operational Git error
- **THEN** iris retains `B` without a deployed notice and surfaces a warning

### Requirement: Plain dogfood release guidance

Iris SHALL explicitly tell agents not to hold dogfood for a user who is away; not to hold it while fixing a problem they found, but to release it, fix the problem, and get back in line with a new claim; and to release it right away when the user is away and Iris asks for it back or says someone is waiting.

This guidance SHALL appear in granted `iris_dogfood_claim` results, successful `iris_set_dogfood` results, grant notices, holder check-ins and final release warnings, the descriptions of `iris_set_dogfood`, `iris_dogfood_claim`, and `iris_dogfood_renew` (including their CLI help), and the shipped Iris skill documentation. This change SHALL affect guidance only, leaving lease enforcement, timers, and queue ordering unchanged.

#### Scenario: Agent receives dogfood

- **WHEN** a claim returns granted, including a repeated claim by the holder, or a grant notice is sent
- **THEN** its message explicitly states all three release rules

#### Scenario: Deployment succeeds

- **WHEN** `iris_set_dogfood` succeeds
- **THEN** its result includes a message explicitly stating all three release rules

#### Scenario: Holder is asked to respond

- **WHEN** Iris sends a holder check-in, a someone-is-waiting notice, or a final release warning
- **THEN** the notice explicitly states all three rules, including immediate release when the user is away

#### Scenario: Agent reads tool and skill instructions

- **WHEN** an agent reads a dogfood claim, renewal, or deployment tool description or the shipped Iris skill
- **THEN** the instructions explicitly state all three release rules

