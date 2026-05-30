## Why

Iris currently mediates every write the worker agent needs to perform against the canonical source repo — push, cherry-pick, PR open/merge, fetch — but offers no first-class motion for two recurring developer workflows: composing a "dogfood" branch from in-flight feature branches for local QA, and shipping a finished feature to `origin/main` (with or without PR ceremony). Today the worker pieces these together by hand, and the existing `iris_push` refusal on the default branch leaves no good answer for personal projects that ship without PR review. The same primitive should serve both Thanx-style PR flows and personal-project skip-PR flows, with a uniform `origin-first` invariant: local `main` only moves via `git fetch`, never by being pushed.

## What Changes

- Add `iris_set_dogfood(sha, manifest)` — hard-resets a per-repo dogfood branch to a worker-supplied SHA, persists a structured manifest, then triggers the existing build/restart machinery.
- Add `iris_ship_feature(branch, via)` where `via ∈ {"pr", "pr-auto"}` — pushes the feature branch to `origin`, opens a PR (existing verb), and for `pr-auto` also approves, merges, fetches, and re-composes the dogfood branch with the shipped feature dropped from the manifest.
- Update `iris_status` to include the active dogfood manifest when one exists.
- Add `dogfood_branch` field to `.iris.toml` schema. Verbs refuse if unset (opt-in per repo).
- The existing `iris_push` default-branch refusal is **unchanged**. Local `main` is never pushed in this model — it only moves via `iris_fetch`.

## Capabilities

### New Capabilities
- `iris-set-dogfood`: hard-reset a configured dogfood branch to a worker-supplied SHA + manifest, persist the manifest, and trigger the existing build/restart machinery.
- `iris-ship-feature`: ship a feature branch to `origin/main` via PR (review or auto-merge), then fetch and re-compose the dogfood branch with the shipped feature dropped from the manifest.

### Modified Capabilities
- `iris-status`: include the active dogfood manifest in the result when one exists.
- `iris-validate-config`: validate the new `dogfood_branch` field.

## Impact

- `internal/config/iris_toml.go` — add `DogfoodBranch string` field to `IrisToml`.
- `internal/verbs/set_dogfood.go` + `internal/verbs/ship_feature.go` — new verbs.
- `internal/mcp/handler_set_dogfood.go` + `internal/mcp/handler_ship_feature.go` — MCP wiring.
- `internal/cmd/*` — CLI subcommands.
- `internal/verbs/status.go` — surface manifest.
- `internal/verbs/validate_config.go` — validate new field.
- Manifest storage: persisted alongside the audit log under the source repo's iris state directory.
- No breaking changes; existing verbs (`iris_push`, `iris_merge_to_master`) untouched.
- New `.iris.toml` field; tooling that parses configs may need updating but the field is optional.
