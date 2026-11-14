## MODIFIED Requirements

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


## ADDED Requirements

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
