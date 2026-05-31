## Why

`iris:reload` and `iris:publish` validate the on-disk `.iris.toml` against the **currently running binary's** schema **before** the repo is brought up to the version that will actually run. The running daemon is the one doing the validation, so any field it does not yet recognise is rejected as an "unknown field" — which makes an additive `.iris.toml` schema change un-deployable while that field is present on disk. This is the deploy hazard documented in the `add-iris-local-toml-overlay` design ("the running daemon must understand every field in the on-disk shared toml"). We hit it for real shipping `add-dogfood-and-ship-verbs`: it took two bootstrap commits to `origin/main` (drop the field, then restore it) plus a manual out-of-band binary swap. The next additive field will hit it again.

## What Changes

- **Reorder reload pre-flight to pull-then-validate.** `iris:reload` fetches + fast-forward-merges the default branch **first**, then loads and validates the **post-pull** `.iris.toml` — the config the rebuilt-and-restarted binary will actually consume. The pre-pull step does only a lenient "peek" to discover the `default_branch` override for the fetch target (falling back to git `origin/HEAD` → `main`). Missing-file, malformed-TOML, and schema-validation refusals all move to **after** the pull. The optional `[pre_flight]` hook also moves to run against the post-pull tree.
- **Validate the published config for `iris:publish`.** `iris:publish` sets the source repo to the **worktree's** HEAD, so the config that will run after the update is the worktree's `.iris.toml`, available before the mutation. `iris:publish` validates the worktree's `.iris.toml` (the config being published) instead of the source repo's stale pre-publish config. No mutation reordering needed — this is the post-update truth, validated safely upfront.
- **Tolerate forward-compatible unknown fields during reload/publish pre-flight.** A tolerant decode mode downgrades unknown `.iris.toml` **fields** from validation errors to warnings, surfaced in the result and audit log. This is the defense-in-depth half that makes the reorder actually deployable: validating a freshly-pulled new field with the old binary's decoder would otherwise read it as "unknown field" and refuse. `schema_version` mismatch stays a **hard** refusal (breaking changes bump the version; additive fields are forward-compatible within a version), and malformed TOML stays a hard error. Tolerant mode is used **only** by reload/publish pre-flight; `iris:validate_config` and other callers stay strict.
- **Dogfood (dev-branch) rebuilds benefit too.** `iris:reload` targets the default branch; a developer rebuilding off their dev branch uses `iris:set_dogfood`, which routes through `Reload` with `no_pull = true`. The reorder is a harmless no-op on that path (no pull), and the unknown-field tolerance is inherited for free — so composing a dogfood SHA that adds a new field also deploys in one shot. No separate `set_dogfood` change is needed.
- The structured `{ valid: false, errors: [...] }` failure shape is preserved — it simply surfaces at a later step (post-pull for reload; against the worktree config for publish). The audit log records `pre_pull_sha`, `post_pull_sha`, and the validation outcome reflecting the post-pull / to-be-published config.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `iris-reload`: pull now happens before `.iris.toml` validation; missing/malformed/schema refusals and the `[pre_flight]` hook move post-pull; unknown fields are tolerated (warned) during pre-flight.
- `iris-publish`: validation targets the worktree's `.iris.toml` (the config being published) rather than the source repo's pre-publish config; unknown fields are tolerated (warned) during pre-flight.

## Impact

- `internal/verbs/reload.go` — reorder pre-flight; lenient pre-pull `default_branch` peek; post-pull load/validate; move `[pre_flight]` hook post-pull.
- `internal/verbs/publish.go` — validate the worktree's `.iris.toml`; tolerant decode.
- `internal/config/iris_toml.go` — add a tolerant decode mode (unknown fields → warnings) returning warnings alongside errors; existing strict `LoadIrisToml`/`DecodeIrisToml` preserved.
- Tests: `internal/verbs/reload_test.go`, `internal/verbs/publish_test.go`, `internal/config/iris_toml_test.go`.
- No change to `.iris.toml` file shape; no change to `iris:validate_config` strictness.
