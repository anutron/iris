## 1. Specs (spec-first)

- [x] 1.1 `iris-dogfood-lease` delta (new capability): lease and ordered queue (FIFO by default, with cut-in `position` and human reorder); claim, renew, release, force-release and queue-move verbs; `holder` and `ahead` visibility; warmer, silence and idle timers; liveness rules; pane-delivery messaging; persistence; timer config.
- [x] 1.2 `iris-set-dogfood` delta: ADDED "Dogfood lease gate" requirement with the `lease_override` input, kept separate from `force`.
- [x] 1.3 `iris-status`, `iris-ship-feature`, `iris-validate-config` deltas: the lease field, the recompose skip, and `[dogfood_lease]` validation.
- [x] 1.4 Resolve design.md Open Questions (a)–(e) with Aaron: approved in Plannotator review round 2 with the recommendations as written.

## 2. Argus client (TDD: httptest fakes first)

- [x] 2.1 `internal/argus/bridge.go`: `GetBridge(ctx, taskID)` returning `{Live, Busy, NeedsInput, LastEventAt, LastHeartbeatAt}`; a 404 maps to a typed "no bridge" result.
- [x] 2.2 Extend `argus.Task` with `Archived bool`; confirm a 404 from `GetTask` is detectable as "task gone".
- [x] 2.3 `internal/argus/notify.go`: `Notify(ctx, taskID, text, deliveryID, deadline)` (POST `/api/tasks/{id}/notify`, `submit: true`) and `CancelNotify(ctx, taskID, deliveryID)` (DELETE).
- [x] 2.4 Liveness classifier: (task, bridge, errors) → dead | busy | silent | unknown-no-bridge | unknown-unreachable, with a table test covering every row of design D5.

## 3. Lease state and persistence (TDD)

- [x] 3.1 `internal/verbs/dogfood_lease.go`: the lease struct (holder, queue, history ≤ 20), load and save of `dogfood-lease.json` in `SourceRepoStateDir` with temp-file-then-rename, and a per-source-repo lease mutex separate from the git mutex.
- [x] 3.2 Timer config: parse `[dogfood_lease]` from the merged overlay (plus the bootstrap read of `.iris.local.toml`), apply defaults 600/30/1800, and fall back to defaults on invalid values with a warning.
- [x] 3.3 State machine as pure functions over (state, now, liveness, config) → (new state, messages to send, deliveries to cancel). Tests use an injected clock and cover:
  - grant, queue, poll and re-claim
  - cut-in `position` (insert, self-move, clamp) and queue-move (reorder, refuse the holder or an unqueued task)
  - `ahead` lists for queued callers and for unqueued refused callers
  - immediate check-in on a new queue join
  - warmer check-in
  - silence → final warning → release with a waiter
  - silence without waiters doesn't release
  - idle-release cap, including a busy holder
  - dead holder release with no grace, and dead queued entries skipped
  - in-flight set_dogfood defers releases
  - an undelivered check-in doesn't count as silence
  - the overdue check-in when a waiter joins
- [x] 3.4 Restart handling: on load, push out `release_at` and outstanding check-in windows to startup + grace; leave `last_contact_at` unchanged. Test with a persisted file and an advanced clock.

## 4. Verbs, handlers, CLI

- [x] 4.1 `DogfoodClaim` (with the `wait_seconds` long-poll, default 0, clamp 0–25; optional cut-in `position`; `holder` and `ahead` in the result), `DogfoodQueueMove`, `DogfoodRenew`, `DogfoodRelease`, `DogfoodForceRelease` (requires `reason`; works when argus is unreachable).
- [x] 4.2 MCP handlers `iris_dogfood_claim` / `_renew` / `_release` / `_force_release` / `_queue_move` and their schemas in `internal/daemon/run.go`.
- [x] 4.3 CLI: `iris dogfood claim|renew|release|force-release|queue-move`.
- [x] 4.4 Lease scheduler goroutine in the daemon: at startup, scan `repos/*/dogfood-lease.json`; tick every ≤ 5s; evaluate each lease under its mutex; send and cancel notify deliveries; persist; stop cleanly on shutdown.

## 5. Integrations

- [x] 5.1 `set_dogfood.go`: add `SetDogfoodOpts.LeaseOverride`. Add the lease gate after the reachability check and before the ancestry check. Record contact at start and finish, mark the call in flight, add the `LEASE OVERRIDE:` warning first, and message the holder. Tests:
  - a non-holder is refused with no side effects; the error and its structured fields carry `holder` (with argus task name), `ahead`, and `position`
  - a free lease still requires a claim
  - the holder deploys
  - an override warns and notifies the holder
  - `force` and `lease_override` are independent in both directions
  - a no-task_id CLI call needs the override
- [x] 5.2 `handler_set_dogfood.go`, `cmd/iris/set_dogfood.go` (`--lease-override`), and the tool schema and description in `run.go`.
- [x] 5.3 `ship_feature.go`: the recompose checks the lease. When the lease is free or held by the shipper, recompose as an internal caller. Otherwise skip with a `recompose_skipped` warning and message the holder. Tests for all three cases.
- [x] 5.4 `status.go`: the `dogfood_lease` field (holder, queue, timers, history, warnings); read-only, and tolerant of a malformed file. Tests.
- [x] 5.5 `internal/config`: the `[dogfood_lease]` block, tagged `local`, with validation (`grace < warmer <= idle_release`, all positive) in `iris:validate_config`. Tests.

## 6. Agent-facing docs

- [x] 6.1 `claude/skills/iris/SKILL.md`:
  - teach `iris_dogfood_claim` → `iris_set_dogfood` → `iris_dogfood_release`
  - answer `[iris]` check-ins with `iris_dogfood_renew`
  - explain the difference between `lease_override` and `force`
- [x] 6.2 Update the `iris_set_dogfood` tool description to say the lease is required.

## 7. Validation

- [x] 7.1 `gofmt`, `go build ./...`, `go vet ./...`, `go test ./...` green.
- [x] 7.2 `openspec validate dogfood-lease-queue --strict` and `openspec validate --all --strict` clean.
- [ ] 7.3 Dogfood it on iris itself: two argus tasks claim, one is queued and receives the hand-off message, and `iris_status` shows the queue. **Deferred:** needs a live argus daemon and a deployed iris with two real tasks, which the implementation sandbox cannot provide; verify after merge and deploy. Covered meanwhile by the fake-argus end-to-end tests.
