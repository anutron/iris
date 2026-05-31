## Why

Setting up `iris:set_dogfood` to dogfood a local daemon (point a `dev` branch at a worker-composed SHA, rebuild, restart) hits two bugs that make it silently do the wrong thing:

1. **`set_dogfood` is overlay-blind.** It resolves `dogfood_branch` with `config.LoadIrisToml`, which reads `.iris.toml` only. But `dogfood_branch` is a `local`-tagged field meant to live in the gitignored `.iris.local.toml`. So a config that passes `iris:validate_config` (which IS overlay-aware) is then refused by `set_dogfood` with `dogfood_branch not configured` — a silent trap. The same overlay-blindness affects `ship_feature`, which reads the local-tagged `ship_ci_timeout_seconds` the same way.

2. **The dogfood build builds the wrong tree.** `set_dogfood` moves the `dev` ref to the composed SHA but never checks it out, then calls `reload` with `no_pull`. Reload's build runs the `[build] command` against whatever is currently checked out — the default branch — so the composed SHA is never built and the daemon restarts on stale code.

## What Changes

### `set_dogfood` and `ship_feature` resolve local fields via the overlay

- `set_dogfood` resolves `dogfood_branch` from the MERGED config via `config.LoadOverlay`, honoring `.iris.local.toml`. Its "not configured" refusal hint now points at `.iris.local.toml`. Overlay taxonomy warnings (e.g. a `local` field left in `.iris.toml`) are propagated into the result's `warnings`.
- `ship_feature`'s CI-timeout resolution reads `ship_ci_timeout_seconds` from the merged config the same way.

### `reload` gains an opt-in build branch (the fix for Bug 2)

- `ReloadInput` gains a `BuildBranch` field. It is empty for every existing caller (no behavior change). When non-empty, reload — after acquiring the per-source-repo lock and before loading config and building — checks out `BuildBranch`, so the build and restart act on the composed SHA's tree (including its own `.iris.toml`), then restores the branch that was checked out on entry. Restore runs on every exit path; a failed restore is surfaced as a warning rather than swallowed. The entry pre-flight is unchanged: reload still requires a clean working tree on the default branch before it will touch anything.
- `set_dogfood` passes the resolved `dogfood_branch` as `BuildBranch` so the dogfood SHA is actually built and deployed.

## Capabilities

### Modified Capabilities

- `iris-set-dogfood` — `dogfood_branch` is resolved from the shared + local overlay, and the reload it triggers builds the composed SHA (the dogfood branch's tree), not the default branch.
- `iris-reload` — adds the optional caller-supplied build-branch checkout/restore.
- `iris-ship-feature` — `ship_ci_timeout_seconds` is resolved from the shared + local overlay.

## Impact

- `internal/verbs/set_dogfood.go`: `LoadOverlay` resolution; pass `BuildBranch`; propagate warnings; updated refusal hint.
- `internal/verbs/ship_feature.go`: `shipCITimeout` uses `LoadOverlay`.
- `internal/verbs/reload.go`: `ReloadInput.BuildBranch`; checkout-for-build + restore.
- Tests: overlay-sourced `dogfood_branch` honored; the dogfood build deploys the composed SHA (not the default branch); original branch restored after; overlay-sourced `ship_ci_timeout_seconds` honored. The dogfood test fixtures now carry `.iris.toml` on the composed SHA's tree, matching real dogfooding (the build runs against that tree).
- No MCP handler or CLI surface changes — `BuildBranch` is an internal `set_dogfood`→`reload` seam, not a new input on either tool.
