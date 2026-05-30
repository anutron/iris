## Why

The just-shipped `add-dogfood-and-ship-verbs` change put `dogfood_branch` (and to a lesser extent `ship_ci_timeout_seconds`) in `.iris.toml`, which is checked in. But these are per-developer workflow choices — different developers on the same repo might want different dogfood branch names, or no dogfood at all. Sharing the field forces every contributor into one workflow and makes the deploy hazard worse: adding a personal-config field to the shared schema requires every developer's daemon to be upgraded in lockstep. The fix is to separate project-wide config from developer-local config.

## What Changes

- Add `.iris.local.toml` as a new gitignored overlay file at the repo root. When present, its fields merge over `.iris.toml`.
- Move `dogfood_branch` from `.iris.toml` to `.iris.local.toml`. Reading the field from `.iris.toml` SHALL produce a validation warning recommending migration. (Not an error — graceful for existing repos.)
- Move `ship_ci_timeout_seconds` to `.iris.local.toml` for the same reason; same migration warning.
- Add `.iris.local.toml` to iris's own `.gitignore`. iris's checked-in `.iris.toml` reverts to its pre-shipping state (no `dogfood_branch`). iris's own `.iris.local.toml` (uncommitted, per-developer) sets `dogfood_branch = "dev"`.
- `iris:validate_config` validates both files: structure-and-cross-checks for `.iris.toml`, plus a separate "shared file contains local-only fields" warning.
- `iris:status` reports the merged config under `config`, plus a new `config_sources` field naming which file supplied each value (so `iris:status` can tell humans "dogfood_branch = dev from .iris.local.toml").
- The dogfood manifest gains a `previous_manifest` field — a 1-deep snapshot of whatever was on the dogfood branch before the current `set_dogfood` overwrote it. Lightweight memory: lets a human or agent ask "what was I composing before?" without scanning the audit log. Not a full history feature; just one step back.
- New `iris:set_local_config` verb — writes `.iris.local.toml` at the source repo root with the supplied per-developer fields, merging with existing contents. Sandboxed workers can use this to set up their own dogfood without leaving the sandbox. Refuses any shared-tagged field (taxonomy enforced).

## Capabilities

### New Capabilities
- `iris-set-local-config`: write `.iris.local.toml` at the source repo root with worker-supplied per-developer fields. Merges with existing contents; refuses shared-tagged fields.

### Modified Capabilities
- `iris-validate-config`: validate `.iris.local.toml` when present; classify per-developer fields; warn when local-only fields appear in shared `.iris.toml`.
- `iris-status`: include the merged config and the source of each field.
- `iris-set-dogfood`: read `dogfood_branch` from the merged config; capture the prior manifest as `previous_manifest` in the new manifest (1-deep memory).
- `iris-ship-feature`: read `ship_ci_timeout_seconds` from the merged config.

## Impact

- `internal/config/iris_toml.go` — add an overlay loader; split per-developer fields into a separate struct or mark them; merge in a deterministic order.
- `internal/config/config_test.go`, `iris_toml_test.go` — new cases for overlay merge, missing overlay, conflict scenarios.
- `internal/verbs/validate_config.go` — surface per-file errors; warn on misplaced fields.
- `internal/verbs/status.go` — expose `config_sources`.
- `internal/verbs/set_dogfood.go`, `internal/verbs/ship_feature.go` — no logic change; read from merged config (which they already do via the existing IrisToml struct).
- iris's own `.iris.toml`: drop `dogfood_branch = "dev"` (back to pre-shipping shape).
- iris's `.gitignore`: add `.iris.local.toml`.
- `README.md`, `SKETCH.md`: document the two-file pattern.
- No breaking change for existing consumers — fields stay readable from `.iris.toml` with a warning.
