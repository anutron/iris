## Context

`iris_set_dogfood` points a source repo's dogfood branch at a composed SHA, records the manifest, and rebuilds/restarts the service. Today any argus task can call it at any time. The per-source-repo git mutex serializes overlapping calls, but it does not stop agent B from replacing agent A's build in the middle of A's test run. The `harden-set-dogfood-safety` change (merged in PR #12, not yet archived) added a `force` input that overrides the commit-dropping ancestry refusal. That guard protects commits. It does not protect whoever is testing right now.

What already exists that this change builds on:

- **State directory:** `SourceRepoStateDir(sourceRepo)` (`~/.iris/audit/repos/<basename>-<hash12>/`) already holds `dogfood-manifest.json`. The lease file goes next to it.
- **Argus client:** `internal/argus/` talks to the argus daemon REST API with a plugin-scoped token: `GetTask`, `ListTasks`, `SetTaskStatus`, `ArchiveTask`.
- **Long-poll pattern:** `iris_run_result` takes `wait_seconds` (default 20, ceiling 25 so a call stays under MCP client timeouts) and returns a handle when the work is still pending.
- **Argus endpoints this design uses** (checked against the argus source, `internal/api/routes.go`):
  - `GET /api/tasks/{id}` returns `status` (`pending` | `in_progress` | `in_review` | `complete`) and `archived` (bool). A 404 means the task is gone.
  - `GET /api/tasks/{id}/bridge` returns `live`, `busy`, `needs_input`, `last_event_at`, `last_heartbeat_at`, and more. It returns 404 `no bridge state for task` when the task has no bridge, which is the case for non-Claude backends or a bridge that never started.
  - `POST /api/tasks/{id}/messages` is task-to-task messaging. The sender is the path-bound task ID, and self-messages are rejected. Iris is not an argus task, so it has no sender identity here.
  - `POST /api/tasks/{id}/notify` is reliable pane delivery: `{text, submit:true, delivery_id, deadline_ms}`. Argus types the text into the recipient's session as a user turn as soon as the session is idle and unfocused, and keeps it pending until then (default deadline 5 minutes, maximum 1 hour). `DELETE /api/tasks/{id}/notify/{delivery_id}` cancels it. Argus's route comments describe it as the plugin channel: "plugins use plugin-scoped tokens to deliver pointer text to recipient tasks."

## Goals / Non-Goals

**Goals:**

- Agents take turns holding a source repo's dogfood branch: one holder at a time, plus a queue that is FIFO by default and can be reordered. Agents may cut in line, and a human may reorder the queue (D1a).
- A waiting or refused caller can always see who holds the lease, by argus task name, and everyone ahead of it in line (D1b).
- `iris_set_dogfood` requires the lease. A separate, explicit `lease_override` lets a caller bypass it, loudly.
- Iris carries the conversation from Aaron's script: grant, "queued, I'll let you know", "still using it?", "timer restarted", "releasing in 30s", "it's yours now".
- A holder that is merely quiet is not evicted while it is visibly working, according to argus's bridge state. A holder that is dead or finished is released at once.
- The lease survives an iris restart, and an iris restart never counts as holder silence.
- Lease state is visible in `iris_status`.

**Non-Goals:**

- Gating `iris_reload` or `iris_publish`. Only `iris_set_dogfood` moves the dogfood branch. See open question (b).
- Authenticating callers. Iris cannot verify that a `task_id` belongs to the caller, the same trust model as every other iris verb. The lease coordinates cooperating agents. It is not a security boundary.
- Preemption of the holder, or a maximum hold time while the holder keeps renewing. Cutting in line reorders the queue only; it never takes the lease from the holder. A holder that keeps answering can keep the lease. The queue is visible in `iris_status` for a human to act on.
- Any change to argus.

## Decisions

### D1. Lease is per source repo and keyed by argus task ID

The dogfood branch belongs to a source repo, so the lease does too. Every verb resolves its target the same way set_dogfood does (`ResolveTarget(task_id)`). All tasks of one argus project therefore share one lease. The holder and queue entries are argus task IDs, because that is the identity iris can message and check for liveness. `claim`, `renew` and `release` require `task_id`. `force_release` accepts a `task_id` or a `path` target, like `iris_status`, so a human at the CLI can use it.

### D1a. Queue order: FIFO by default, agents may cut in line, a human may reorder

Aaron's review asked for both: he can rearrange the queue himself, and an agent that wants to cut in line is allowed to.

- **Agent cut-in.** `iris_dogfood_claim` takes an optional `position` (1-based). With no `position`, the claimant is appended (FIFO). With one, it is inserted at that position, clamped to `[1, queue_length + 1]`, and everyone at or after that position moves back one. An already-queued agent calling claim with a `position` moves itself. No justification is required. Iris trusts the agent, and the cut is recorded in lease history and shown in `iris_status`, so a human can see it and reverse it.
- **Human reorder.** A new `iris_dogfood_queue_move` verb (`iris dogfood queue-move <task> <position>` on the CLI) moves any queued task to any position. It resolves the repo like `iris_status` (`task_id` or `path`), so the CLI works without a task.
- **Messages after a reorder.** Tasks whose position changed are not messaged. They see their new position on their next claim poll. Messaging every shuffle would spam agents.
- **The holder is untouched.** Cutting in line or reordering never affects the holder. Only the queue is reordered, and position 1 is next in line, not the holder.
- **Alternative rejected:** priority classes. They add policy without buying anything over explicit positions.

### D1b. Callers can always see the holder and who is ahead

Every response that tells a caller it doesn't have the lease (`claim` → queued, the `set_dogfood` lease refusal, the `renew` refusal) includes:

- `holder` as `{task_id, task_name}`. `task_name` is the argus task name, captured from `GET /api/tasks/{id}` at claim time and refreshed on each liveness check.
- `ahead`, the ordered list of `{task_id, task_name, position}` for every queued task in front of the caller.

For a caller that isn't queued (a set_dogfood refusal before any claim), `ahead` is the whole queue, because that is who it would wait behind if it claimed now.

### D2. Messaging goes through argus pane delivery, and agents reply through iris verbs

Options considered:

- **`POST /api/tasks/{id}/messages` (task inbox).** Rejected as the primary channel. It needs a sender task, and iris isn't one. Borrowing another task's identity (such as the queued claimant) would show messages from the wrong party, and "self-messages rejected" rules out sending as the holder. The REST path also doesn't nudge the recipient's terminal, so a busy agent would never notice the message.
- **Have argus give iris a task identity.** Rejected for v1 because it changes argus. This change avoids argus changes.
- **`POST /api/tasks/{id}/notify` (chosen).** Built for plugins. It shows up as a user turn in the agent's session, which is what Aaron's script needs ("Iris → Agent1: still using it?"). Delivery waits until the session is idle, which fits the liveness rules: an agent busy running tests isn't interrupted, and busy counts as alive anyway (D5).

Replies are iris verb calls, not chat replies. Every message names the exact tool to call: `iris_dogfood_renew` for "yes", `iris_dogfood_release` for "done". This also covers Decision 4's "any iris call from the holder resets the clock". Contact is defined as any of these from the holder:

- `iris_dogfood_renew`
- `iris_dogfood_claim`
- `iris_set_dogfood`, both at the start and the end of the call

The holder's free-text chat reply is not seen by iris.

Delivery IDs follow `iris-lease-<repo-slug>-<event>-<unix-nanos>`, which matches argus's `[A-Za-z0-9_-]{1,128}` rule. Deadlines:

- Check-ins expire after the warmer interval.
- Grants and release notices expire after 1 hour, the argus maximum.

When contact arrives, iris cancels any check-in or final-warning delivery still pending (`DELETE …/notify/{id}`), so the agent doesn't later see a stale "still using it?".

Message texts (wording can be tuned in implementation; the content is required):

| Event | Text |
|---|---|
| Grant | `[iris] You have the dogfood lease for <repo> (branch <dogfood>). <warmer>m timer is running — any iris_set_dogfood / iris_dogfood_renew call resets it. Call iris_dogfood_release when you're done.` |
| Check-in | `[iris] Still using dogfood for <repo>? Reply by calling iris_dogfood_renew (yes) or iris_dogfood_release (done).` plus, when someone is waiting, `<N> agent(s) waiting.` |
| Final warning | `[iris] No answer — releasing the dogfood lease for <repo> in <grace>s unless you call iris_dogfood_renew.` |
| Released (silence, idle, dead, force) | `[iris] Released your dogfood lease for <repo> (<reason>). Call iris_dogfood_claim if you need it again.` |
| Overridden | `[iris] <caller> deployed to dogfood for <repo> with lease_override while you hold the lease; the running tree is now <sha>.` |
| Recompose skipped | `[iris] <shipper> shipped <branch> to <default>; dogfood recompose was skipped because you hold the lease. Recompose when convenient.` |

The queued response ("busy, I'll tell you when it frees") is the synchronous `claim` result. It isn't a pushed message.

### D3. State machine

Persisted state for each source repo:

- `holder`: `{task_id, task_name, granted_at, last_contact_at, checkin_sent_at?, final_warning_at?, release_at?, in_flight}`, or `null`.
- `queue`: ordered list of `{task_id, task_name, enqueued_at}`.
- `history`: the last 20 transitions.

There is one invariant: **if the queue is non-empty, there is a holder.** Granting is immediate whenever the lease is free.

The timers are the warmer `W`, the grace `G` and the idle-release window `I`. The scheduler evaluates each lease every few seconds (tick ≤ 5s).

1. **Claim.**
   - Lease free: grant, set `last_contact_at = now`, and message the grant.
   - Caller already holds the lease: count it as contact and return granted.
   - Caller already queued: return the current position (after moving it, if it passed a `position`), with no new check-in.
   - Otherwise: first run the liveness check on the holder (D5). If the holder is dead, release it and re-evaluate, so the claimant may be granted on the spot. If not, insert the claimant into the queue (at the end, or at its requested `position`, D1a) and **trigger an immediate check-in** on the holder, unless a check-in is already outstanding.
2. **Warmer.** When `now ≥ last_contact_at + W` and no check-in is outstanding, run the liveness check (dead → release), then send a check-in and record `checkin_sent_at`.
3. **Silence with waiters.** The queue is non-empty, a check-in is outstanding, and there has been no contact for `G` since `checkin_sent_at`. Run the liveness check:
   - Dead: release immediately.
   - Busy: defer (D5).
   - Silent: send the final warning and set `release_at = now + G`.
   At `release_at`, run the liveness check again. If the holder is still silent, release it and grant the next live claimant.
4. **Silence without waiters.** A missed check-in releases nothing, and no final warning is sent. The next check-in goes out at `checkin_sent_at + W`.
5. **Idle release.** When `now ≥ last_contact_at + I`, release the lease whether or not anyone is waiting and whether or not the holder is busy. This is the hard cap, so a holder can't keep the lease forever without answering. See open question (a).
6. **Contact.** Contact from the holder resets `last_contact_at`, clears `checkin_sent_at`, `final_warning_at` and `release_at`, and cancels any pending deliveries.
7. **Release of any kind.** Notify the old holder (unless it released voluntarily). Pop queue entries until one passes the liveness check; dead entries are dropped. Grant that entry and message it. If no live entry remains, the lease becomes free.
8. **In-flight set_dogfood.** While the holder has an `iris_set_dogfood` call running (`in_flight`), every release path except force-release is deferred. Reloads can take minutes, and the holder is clearly not idle.

Why two grace windows (check-in → final warning → release) instead of one: Aaron's script has two silences ("still busy? → silence → releasing in 30s → silence → released"). Reusing `G` for both keeps it to one knob. The default total is 60s after a check-in with someone waiting.

### D4. Lease file, atomic writes, and restart behavior

The lease is stored at `<SourceRepoStateDir>/dogfood-lease.json` and written with temp-file-then-rename on every transition. It has its own mutex per source repo, separate from the git mutex, which set_dogfood and reload hold for minutes. On startup the daemon scans `~/.iris/audit/repos/*/dogfood-lease.json` and starts the scheduler for each lease that has a holder.

**A restart never counts as holder silence.** On load, any `release_at` or outstanding check-in whose deadline passed while iris was down is pushed out: `release_at = max(release_at, startup + G)`, and an outstanding check-in has its `G` reply window restarted from startup. `last_contact_at` is not changed, so the idle-release cap still holds across restarts.

### D5. Liveness rules, checked before any release on silence

At every decision point (claim against a held lease, a check-in coming due, `G` expiring, `release_at` arriving, granting to a queued entry), iris asks argus:

| Argus says | Treated as | Effect |
|---|---|---|
| `GET /api/tasks/{id}` → 404, or `status=complete`, or `archived=true` | dead | Release immediately, no grace, no final warning. A queued entry is dropped. |
| Bridge `live=false` | dead | Same as above. |
| Bridge `busy=true` (and live) | alive | Don't release on silence. Re-evaluate on the next tick. The idle-release cap (D3.5) still applies. |
| Bridge `needs_input=true` (and not busy) | silent | Normal silence rules. |
| Bridge 404 (no bridge: non-Claude backend or no bridge yet) | unknown | Plain silence rules. Task status is still checked. |
| Argus unreachable or 5xx | unknown | **Never release on silence.** Iris can't tell, and probably couldn't deliver the check-in either. Retry next tick. The idle-release cap still applies. |

**A check-in that wasn't delivered** (notify returned an error other than 404) doesn't start the `G` countdown. Iris retries delivery on the next tick. A 404 from notify means the task is gone, so it is treated as dead.

### D6. `lease_override` is a separate input from `force`

`force` answers "may I drop commits?". `lease_override` answers "may I deploy while someone else is testing?". They are independent, neither implies the other, and a deploy may need both. With `lease_override`:

- set_dogfood proceeds.
- It puts `LEASE OVERRIDE: …` first in `warnings`, naming the holder or "no holder".
- It messages the current holder (if any, and if it isn't the caller).
- It does not change the lease: the holder keeps it, and the queue is untouched.

A caller with no `task_id` (a human at the CLI, or the iris-on-iris default) can never hold the lease, so it must pass `lease_override`.

### D7. Delta strategy for `iris-set-dogfood`

`harden-set-dogfood-safety` is not archived yet, and its delta MODIFIES the `iris:set_dogfood` verb requirement. If this change also modified that requirement, its block would have to restate harden's text and would depend on archive order. Instead, this change ADDS a separate "Dogfood lease gate" requirement to `iris-set-dogfood`, which defines `lease_override`, the refusal, and the contact semantics. The two changes then archive cleanly in either order.

### D8. Timer config: a `[dogfood_lease]` block tagged `local`

```toml
[dogfood_lease]
warmer_seconds = 600        # 10m check-in cadence
grace_seconds = 30          # reply window after a check-in, and after the final warning
idle_release_seconds = 1800 # 30m with no word → released
```

The block is read from the merged overlay at the source root (`.iris.toml` + `.iris.local.toml`), not from the composed SHA. It is tagged `local` like `dogfood_branch`, because it is host policy for this dogfood host. Like `dogfood_branch`, it is read from `.iris.local.toml` when `.iris.toml` is absent (the bootstrap case). Validation:

- All three values must be positive integers when present.
- `grace_seconds < warmer_seconds ≤ idle_release_seconds`.

If the block is missing, the defaults apply. Config is re-read each time the scheduler evaluates a lease, so edits take effect without a restart.

### D9. ship_feature's internal recompose

`recomposeAfterShip` calls `SetDogfood` internally with the shipping task's ID. With the lease gate in place:

- If the lease is free or held by the shipping task, the recompose runs. It goes through the gate as an internal caller, with no override warning, and counts as contact for the holder.
- If the lease is held by another task, the recompose is skipped with a `recompose_skipped` warning whose message names the holder (the existing warning code; no new code), and the holder is messaged that it may want to recompose.

The alternative of always overriding would rebuild and restart the dogfood service under someone mid-test, which is the exact failure this change exists to prevent. Skipping is harmless: the shipped feature is now on the default branch and still layered in dogfood until the next recompose. See open question (c).

## Risks / Trade-offs

- **[Agents ignore the protocol and never release.]** A holder that keeps renewing, or that sits busy, can keep the lease past the warmer. → The idle-release cap ends it (30m without contact). `iris_status` shows the queue, and `force_release` is the human or coordinator escape hatch.
- **[Pane delivery types into the agent's session as a user turn.]** That interrupts the agent's flow at its next idle point. → Messages are short, prefixed `[iris]`, and name the exact tool to call. Pending check-ins are cancelled as soon as contact arrives, so stale prompts don't land.
- **[`task_id` is self-asserted.]** An agent could claim or renew as someone else. → Same trust model as every other iris verb. The lease coordinates cooperating agents and doesn't defend against hostile ones.
- **[Argus down stalls the queue.]** Iris never releases on silence it can't verify. → The idle-release cap bounds the stall, and `force_release` works without argus. It still messages on a best-effort basis.
- **[Breaking change for existing callers of `iris_set_dogfood`.]** Agents that worked before are now refused. → The refusal message gives the exact remedy (`iris_dogfood_claim`, or `lease_override`). The iris agent skill and the tool description are updated in the same change.
- **[Busy is not progress.]** An agent stuck in a loop reads as `busy=true` forever. → Hard cap via idle release (open question (a)).

## Migration Plan

1. Ship the lease verbs, the scheduler and the status field.
2. In the same release, turn on the set_dogfood gate. Iris is single-user, and a feature flag adds more risk than it removes.
3. Update the iris agent skill (`claim` → `set_dogfood` → `release`) and the `iris_set_dogfood` tool description in the same PR.
4. Rollback: revert the release. `dogfood-lease.json` files left behind are ignored by older iris builds.

## Open Questions

Each question has a recommended answer. The specs encode the recommendation so the change is complete. Aaron can override any of them.

- **(a) Should `busy=true` defer a release forever, or is there a hard cap?** Recommendation: **cap it.** Busy defers a release on silence, but the idle-release window (default 30m since the last contact) applies even to a busy holder. A looping agent can't keep the lease forever. The specs encode one uniform rule: idle release fires whether or not anyone is waiting and whether or not the holder is busy. Alternative: cap only when someone is waiting, and let a busy holder keep the lease indefinitely when nobody is. That's harmless, but it leaves a stale holder in status.
- **(b) Does the lease cover only `set_dogfood`, or also `reload` and `publish` against the dogfood branch?** Recommendation: **only `set_dogfood` for v1.** It is the only verb that moves the dogfood branch, and reload's `BuildBranch` is internal-only. A follow-up could make `iris_reload` and `iris_publish` add a warning, and message the holder, when they restart a system whose dogfood lease is held by another task, since restarting from the default branch replaces the running dogfood build. Warn, don't gate. Left out of this change's specs.
- **(c) How does `iris_ship_feature`'s internal recompose interact with the lease?** Recommendation, as in D9 and encoded in the `iris-ship-feature` delta: run it when the lease is free or held by the shipper; otherwise skip it with a `recompose_skipped` warning naming the holder, and message the holder.
- **(d) Should `set_dogfood` auto-claim when the lease is free and nobody is queued?** That would save a round trip. Recommendation: **no for v1**, following Decision 1 (holding the lease is required) strictly. The refusal names `iris_dogfood_claim`, so the cost is one extra call. Revisit if it causes friction.
- **(e) Who may call `force_release`?** Iris can't tell a coordinator from a worker. Recommendation: anyone. It requires a `reason`, records it in lease history, and messages the evicted holder.
