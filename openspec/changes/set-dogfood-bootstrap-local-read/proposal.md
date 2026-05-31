## Why

`iris:set_dogfood` resolves `dogfood_branch` via `config.LoadOverlay`, which returns no document whenever `.iris.toml` is absent at the source repo root (it treats `.iris.local.toml` strictly as an overlay on a required base). During dogfood bootstrap the default branch (e.g. argus `master`) has no `.iris.toml` yet — it lives on the dogfood branch — so `dogfood_branch = "dev"` in `.iris.local.toml` is never read and `set_dogfood` bails at config resolution. This is a chicken-and-egg: `dogfood_branch` is the pointer to the branch that carries `.iris.toml`, yet reading it is gated behind `.iris.toml` existing. The documented workaround (check out `dev` first) does not work either, because `set_dogfood`'s reload pre-flight requires HEAD to be on the default branch before it builds the dogfood branch. So bootstrap dogfooding is blocked.

## What Changes

- `iris:set_dogfood` SHALL resolve `dogfood_branch` directly from `.iris.local.toml` when it is not available from the merged overlay (i.e. when `.iris.toml` is absent at the source repo root). `dogfood_branch` is a `local`-tagged field and is precisely the pointer to the branch that carries `.iris.toml`, so reading it must not be gated behind `.iris.toml`. When the merged overlay does provide `dogfood_branch` (the `.iris.toml`-present case), behavior is unchanged.

With this change, bootstrap `set_dogfood` works from a bare default branch in one call: it reads `dogfood_branch` from `.iris.local.toml`, points the dogfood branch at the composed SHA, and the reload checks out that branch (which carries `.iris.toml`) to build, then restores the entry branch — no manual pre-checkout, the source repo stays on its default branch.

## Capabilities

### Modified Capabilities

- `iris-set-dogfood` — resolves `dogfood_branch` from `.iris.local.toml` even when the source root has no `.iris.toml` (bootstrap).

## Impact

- `internal/config/iris_toml_overlay.go`: a lenient `PeekLocalDogfoodBranch(repoRoot)` reading only `dogfood_branch` from `.iris.local.toml` (mirrors `PeekDefaultBranch`).
- `internal/verbs/set_dogfood.go`: when the overlay yields no `dogfood_branch`, fall back to `PeekLocalDogfoodBranch` before refusing.
- Tests: `set_dogfood` succeeds when the default branch has no `.iris.toml` but `.iris.local.toml` declares `dogfood_branch` and the composed SHA carries `.iris.toml`; existing `.iris.toml`-present behavior preserved.
- `claude/skills/iris/config-authoring.md`: the bootstrap runbook notes that `set_dogfood` reads `dogfood_branch` from `.iris.local.toml` even on a bare default branch (no pre-checkout needed).
