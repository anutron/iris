## Why

Several agents can run `iris_set_dogfood` against the same source repo at the same time, and each call replaces whatever the others just deployed. An agent partway through testing its build finds a different tree running, and nothing tells it why. The ancestry guard from the earlier hardening change (the `force` input) catches dropped commits, but it doesn't say who is testing right now. Agents need to take turns holding the dogfood branch, with a fair queue, and iris should release the branch when a holder goes quiet or dies.

## What Changes

- **New lease and queue per source repo.** At most one argus task holds the dogfood lease at a time. A `claim` is granted right away if the lease is free. Otherwise the caller joins the queue (FIFO by default) and gets back its position, the holder's argus task name, and the list of everyone ahead of it. `claim` takes an optional `wait_seconds` so the caller can wait for the grant, the same long-poll style as `iris_run_result`.
- **Queue can be reordered.** An agent may cut in line by passing a `position` to `claim`. A human or coordinator can move any queued task with `iris_dogfood_queue_move`. The holder is never preempted.
- **New verbs:**
  - `iris_dogfood_claim`: take the lease, or join the queue.
  - `iris_dogfood_renew`: the "yes, still using it" reply.
  - `iris_dogfood_release`: give up the lease, or leave the queue.
  - `iris_dogfood_force_release`: lets a human or coordinator release a stuck holder.
  - `iris_dogfood_queue_move`: lets a human or coordinator reorder the queue.
- **Iris messages agents.** Iris sends grants, check-ins ("still using it?"), the final "releasing in 30s" warning, and release notices through argus's reliable pane delivery (`POST /api/tasks/{id}/notify`). Agents answer by calling iris verbs, not by replying in chat.
- **Warmer timer (default 10 minutes).** Iris checks in with the holder periodically. Any reply, or any iris call from the holder (`set_dogfood`, `claim`, `renew`), restarts the clock. When someone joins the queue, iris checks in with the holder right away.
- **Release on silence, only when someone is waiting.** If the queue is non-empty and the holder doesn't answer a check-in, iris sends a "releasing in 30s" warning. After the grace period passes in silence, it releases the lease and grants it to the next agent in line. If nobody is waiting, a missed check-in releases nothing. The lease is released only after the idle-release window (default 30 minutes) passes with no word from the holder.
- **Silence doesn't mean the holder is dead.** Before releasing on silence, iris checks argus for the holder's real state:
  - A holder whose task is complete or archived, or whose session is dead (`live=false`), is released immediately, with no grace period.
  - A busy holder (`busy=true`) counts as alive.
  - A holder stuck on a prompt (`needs_input=true`) counts as silent.
  - A holder with no bridge state falls back to the plain silence rules.
- **Lease state persists** as `dogfood-lease.json` in the same per-source-repo state directory as `dogfood-manifest.json`, and survives an iris restart.
- **Timers are configurable** in a new `[dogfood_lease]` block of the merged `.iris.toml` overlay (`warmer_seconds = 600`, `grace_seconds = 30`, `idle_release_seconds = 1800`).
- **BREAKING: `iris_set_dogfood` requires the lease.** It refuses a caller (`task_id`) that doesn't hold the dogfood lease, unless a new `lease_override` input is passed. The refusal names the holder (argus task name) and lists everyone queued ahead. With `lease_override`, the deploy goes ahead with a prominent warning, and iris tells the current holder its tree was replaced. `lease_override` is a separate input from the existing `force` ancestry override, and neither one implies the other.
- **`iris_status` reports the lease:** the holder, the queue, and the timers.
- **`iris_ship_feature`'s internal recompose respects the lease.** It deploys when the lease is free or held by the shipping task. Otherwise it skips the recompose and returns a `recompose_skipped` warning.

## Capabilities

### New Capabilities

- `iris-dogfood-lease`: the per-source-repo dogfood lease and FIFO queue. Covers the claim (with optional cut-in `position`), renew, release, force-release and queue-move verbs, the persisted lease state, the warmer, grace and idle-release timers, iris-to-agent messaging over argus pane delivery, argus liveness checks before a release on silence, and the `[dogfood_lease]` timer config.

### Modified Capabilities

- `iris-set-dogfood`: adds a lease gate. A caller that doesn't hold the lease is refused unless it passes the new `lease_override` input. A set_dogfood call from the holder counts as contact with iris.
- `iris-status`: the result gains a `dogfood_lease` field with the holder, the queue, and timer deadlines.
- `iris-ship-feature`: the post-ship recompose skips with a warning when another task holds the dogfood lease.
- `iris-validate-config`: validates the new `local`-tagged `[dogfood_lease]` timer block.

## Impact

- **New code:** `internal/verbs/dogfood_lease.go` (state, the persisted JSON, the state machine) and a lease scheduler goroutine in the daemon that drives the timers.
- **`internal/argus/`:** new client methods for `GET /api/tasks/{id}/bridge`, `POST /api/tasks/{id}/notify`, and `DELETE /api/tasks/{id}/notify/{delivery_id}`. `GetTask` is reused for status.
- **`internal/verbs/set_dogfood.go`:** the lease gate and the `LeaseOverride` option.
- **`internal/verbs/ship_feature.go`:** the recompose checks the lease.
- **`internal/verbs/status.go`:** the `dogfood_lease` field.
- **`internal/mcp/`:** five new handlers, plus the `lease_override` input on the set_dogfood handler.
- **`internal/daemon/run.go`:** tool schemas.
- **`cmd/iris/`:** CLI subcommands and flags.
- **`internal/config/iris_toml.go`:** the `[dogfood_lease]` block, tagged `local`, with validation.
- **Agent-facing docs:** the iris agent skill must teach the claim, renew and release flow, and explain that set_dogfood now requires the lease.
- **No change to argus.** Iris uses existing argus endpoints.
