## Context

The lease currently serializes by task identity. The proposed direction is to let callers describe the commit they need, without predicting the holder's future deploy or changing exclusive deployment ownership. Existing per-repo lease locking and persisted notice tracking remain the foundation. The branch is moved before reload; branch containment is therefore a statement about branch contents, not a new runtime health guarantee.

## Goals / non-goals

**Goals:**

- Satisfy requests whose required commit is already contained in the configured dogfood branch.
- Remove every satisfied waiter after a successful deployment, preserving the remaining queue order and holder.
- Preserve task-only claims, lease ownership, notice cancellation, persistence, and restart behavior.

**Non-goals:**

- Predicting a holder's intended SHA, merging work, automatic recomposition, or sharing deployment ownership.
- Preventing `--force`, watching arbitrary external branch changes, or certifying the running service's health.

## Decisions

### Resolve once, compare immutable commits

Resolve a supplied `sha` as a commit in the host source repo before changing lease state. Reject option-like or invalid inputs without queue, timer, or notice changes. Persist the full resolved commit on a queued entry; `main` advancing later does not change that request. A queued re-claim with a new explicit `sha` replaces the request; an omitted `sha` retains a previously stored request so legacy poll calls do not discard it. Pin that effective SHA for the lifetime of a waiting claim, even if deployment reconciliation removes its queue entry between polls. Entries that never requested a commit retain today's behavior. Reading an absent dogfood branch means no containment; operational Git failures must not be mistaken for successful containment.

Use exact Git ancestry (`merge-base --is-ancestor requested currentHead`), not equality alone or patch equivalence. Computing containment belongs in the I/O shell, with supplied results/callbacks consumed by the state machine for deterministic tests.

### Check before granting as well as queueing

For non-holders, check containment before liveness/timer transitions that might grant the caller. A positive check removes an existing queue entry if needed, cancels obsolete notices, and returns `already_deployed`. The result has position 0, empty ahead, no next-checkin time, current holder or null, current queue length, and resolved `sha`. It grants no lease, sends no redundant grant notice, and does not contact the holder. If removal empties the queue, cancel the release-on-silence warning and schedule as existing last-waiter removal does.

An existing holder continues to return `granted` and record contact even when its requested commit is contained. Silently relinquishing an existing holder's lease could interrupt ongoing testing; this also avoids returning a no-lease result to a task that still owns one. A free lease short-circuits just like a busy one because no deployment ownership is needed when the requested content is already present.

### Satisfy the entire queue on deployment and before handoff

After `set_dogfood` completes its reload successfully, re-read the current configured branch head and re-evaluate every SHA-bearing queue entry under the lease lock. Remove all satisfied entries, record history, cancel their obsolete notices before posting new satisfaction notices, and preserve the holder and remaining queue order. This applies to lease overrides, forced deploys, and internal recomposition too. Do not announce success when reload fails. A failed queue reconciliation after a successful deploy is surfaced as a warning rather than claiming the deploy failed.

Also reconcile before granting the next waiter on release, force-release, or automatic holder eviction, and when polling an existing queued claim. This prevents a waiter acquiring an unnecessary lease if release follows a deploy or branch movement. It is idempotent: already removed entries receive no duplicate notice. Reconciliation errors retain unsatisfied/unknown entries and use the existing warning channel; never remove a waiter without proven containment. Force-release must retain its existing availability when Argus is unreachable.

Do not hold the lease lock across reload or acquire the source Git lock inside a lease operation. Read the immutable branch-head snapshot and test ancestry within the lease transaction; branch movement can race with this observation, just as a forced deploy can invalidate it immediately afterward. This avoids reversing the current Git/lease lock order and blocking claim polling for a build.

### Reuse persisted notice lifecycle

Add a `deployed` notice kind using the existing `[iris]` prefix, generation, issued timestamp, sanitized text, unique delivery ID, and one-hour deadline. Say “your commit <sha> is now live on <branch>; no lease needed”, name `iris_status` for confirmation, and explain that `--force` or an external branch rewrite can remove it. Persist the notice even though its task is no longer queued.

A later claim or release by that recipient supersedes its pending deployed notice, including after restart. A new grant cancels it through the existing all-notices cancellation. Ordinary unrelated holder contact does not cancel another task's notice. If a forced deploy removes a notified commit, cancel any still-pending deployed notice for that commit before delivering new notices; retain the SHA with the notice to support this check. No automatic requeue follows satisfaction or a forced rollback.

### Scope of the deployed guarantee

The existing descendant-only guard is sufficient to keep an ancestor present across ordinary Iris deploys, including deploys by later lease holders. Lease ownership alone provides no such guarantee. `--force` deliberately bypasses that guard, and external Git ref changes bypass Iris entirely. Both response and notice describe the exception. “Live” uses the existing dogfood-branch convention; it is not a new assertion that a binary has passed health checks. Successful deploy notifications follow reload success; direct claims inspect branch contents as requested.

## Risks / trade-offs

- Git ancestry checks grow with SHA-bearing waiters → evaluate each distinct requested commit once per branch snapshot and leave legacy entries untouched.
- Forced or external branch changes invalidate earlier observations → explicitly qualify messages, cancel tracked notices on Iris branch changes, and tell agents to confirm status before acting.
- Restart or reconciliation failure between deployment and notification → persisted queue requests remain available for claim polling and pre-handoff reconciliation; report errors and never infer containment from a failed Git command.
- A ref moves while being resolved → pin its resolved commit once per explicit claim rather than re-resolving during wait polling.

## Migration plan

- Add optional `sha` fields to queue entries and deployment notices; missing fields decode as legacy task-only state.
- Ship CLI/MCP support and the updated agent skill together. Rollback preserves legacy queue operation but loses commit-aware satisfaction until redeployed.
- Use TDD against the delta scenarios, then run all Go tests and strict OpenSpec validation. Archive only after CI, review, and pre-archive audit pass.

## Open questions

None proposed; Aaron reviews the behavior and exceptions before implementation.
