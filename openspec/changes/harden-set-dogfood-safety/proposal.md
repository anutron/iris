## Why

`iris:set_dogfood` has two coupled safety gaps that surface once a project is actually being dogfooded:

1. **Worktree-guard bug (the regression this effort started from).** The ref move at `set_dogfood.go:153` uses `git branch -f <dogfood> <newSHA>`. Git refuses to force-move a branch that is checked out in any worktree ("cannot force update the branch '<x>' used by worktree ..."). On a live dogfood host the dogfood branch is frequently the checked-out branch, so the deploy fails at the ref move.

2. **No ancestry safety.** `previous_sha` is read and returned but never checked. If the worker hands iris a `newSHA` that is not a descendant of the current dogfood SHA, the deploy silently drops the commits that were on the branch. Nothing warns or refuses.

Both are hardening fixes to `internal/verbs/set_dogfood.go`. They preserve the verb's documented philosophy — "iris is dumb, the agent composes." Iris still only CHECKS and REFUSES; it never auto-composes or merges.

## What Changes

### Fix 1 — worktree-guarded hybrid ref move

- Before moving the dogfood ref, detect whether the dogfood branch is checked out in any worktree of the source repo (parse `git worktree list --porcelain`).
- If it is NOT checked out anywhere: keep the existing `git branch -f <dogfood> <newSHA>` (preserves the current ref-only contract for the common case).
- If it IS checked out in a worktree: run `git reset --hard <newSHA>` in that worktree instead.
- `git update-ref` is explicitly NOT used — it moves the ref out from under a checked-out worktree, leaving HEAD/index disagreeing with the tree (phantom staged diffs).
- The create-when-absent path (`git branch <dogfood> <sha>`, no `-f`) is unaffected.

### Fix 2 — ancestry-check + refuse/warn

- Inside the held lock, between reading `previous_sha` and moving the ref: if `previous_sha` is non-empty AND `newSHA` is NOT a descendant of `previous_sha` (`git merge-base --is-ancestor <previous_sha> <newSHA>` exits non-zero), the deploy would drop commits.
- In that case iris REFUSES with a clear error naming how many commits would be dropped (`git rev-list --count <newSHA>..<previous_sha>`) — UNLESS a new `force` boolean input is passed.
- When `force` overrides, iris proceeds but appends a prominent entry to `warnings`.
- `previous_sha` is always surfaced prominently.
- Iris does NOT implement auto-compose/merge. The remedy is the agent's: recompose on the current dogfood SHA, or pass `force` to intentionally drop.

The refusal happens before the manifest write, so a refused deploy has no side effects (no manifest, no git mutation, no reload).

### `force` input, end to end

- Added to the `SetDogfoodOpts` struct, the MCP handler, the CLI flag, and the `iris_set_dogfood` tool schema. Default `false`.

## Capabilities

### Modified Capabilities

- `iris-set-dogfood` — the ref move is worktree-guarded (hybrid `branch -f` / `reset --hard`); a `newSHA` that would drop commits is refused unless `force` is passed (with a warning); a new optional `force` input is added.

## Impact

- `internal/verbs/set_dogfood.go`: `SetDogfoodOpts.Force`; ancestry check; worktree-guarded ref move; force warning.
- `internal/mcp/handler_set_dogfood.go`: accept and forward `force`.
- `cmd/iris/set_dogfood.go`: `--force` flag.
- `internal/daemon/run.go`: `force` in the `iris_set_dogfood` input schema (and description).
- `internal/verbs/ship_feature.go`: the post-ship recompose is an intentional commit-dropping deploy (it rebuilds the dogfood tip with the just-shipped feature removed), so its internal `SetDogfood` call passes `force = true` to opt past the new ancestry refusal. No change to ship_feature's contract; the drop is still reported via `RecomposeResult`.
- Tests: worktree-guard ref move succeeds when the dogfood branch is checked out; non-descendant `newSHA` refused without `force` and allowed (with warning) with `force`; descendant `newSHA` proceeds; existing overlay-resolution and composed-SHA-build tests stay green.
