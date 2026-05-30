## 1. Field taxonomy

- [x] 1.1 Add a `kind` (or equivalent) tag to each field in the `IrisToml` struct, classifying it as `shared` or `local`. Define an exhaustive helper enumerating which fields are which.
- [x] 1.2 Mark `dogfood_branch` and `ship_ci_timeout_seconds` as `local`; everything else (`schema_version`, `default_branch`, `[build]`, `[restart]`, `[pre_flight]`, `[verify]`, `[post_merge]`) as `shared`.
- [x] 1.3 Unit-test the taxonomy helper: every field is classified; no field is unclassified.

## 2. Overlay loader

- [x] 2.1 Add a loader that reads `.iris.toml` then `.iris.local.toml` (if present) and merges them. Absence of the local file is silent.
- [x] 2.2 Implement merge semantics: for `local`-tagged fields, the local file's value wins; for `shared`-tagged fields, the shared file's value wins and the local file's value is recorded as a `shared_field_in_local_config` warning.
- [x] 2.3 Track per-field provenance during the merge so it can be exposed via `iris:status`.
- [x] 2.4 Unit tests for: local-only field overlay, shared-and-local same field (local wins + no warning), shared-only field with local-file override (shared wins + warning), missing local file (silent), malformed local file (structured error).

## 3. Validation updates

- [x] 3.1 In `iris:validate_config`, run the overlay loader and surface its warnings. Emit `local_field_in_shared_config` per local-tagged field present in `.iris.toml` with a migration hint pointing at `.iris.local.toml`.
- [x] 3.2 Emit `shared_field_in_local_config` per shared-tagged field present in `.iris.local.toml`.
- [x] 3.3 Malformed `.iris.local.toml` surfaces a structured error (file + line/column when available) without losing `.iris.toml`'s fields.
- [x] 3.4 Update `validate_config_test.go` with golden cases for every spec scenario in `specs/iris-validate-config/spec.md`.

## 4. Status integration

- [x] 4.1 Extend `verbs.Status` to return `config_sources` keyed by top-level field name (`"shared"` or `"local"`).
- [x] 4.2 Omit fields that were unset in both files (don't emit `"none"`).
- [x] 4.3 When neither file exists, `config_sources` is an empty object (`{}`), not absent.
- [x] 4.4 Update `status_test.go` per the scenarios in `specs/iris-status/spec.md`.

## 5. Manifest `previous_manifest`

- [x] 5.1 Extend `DogfoodManifest` struct in `internal/verbs/dogfood_manifest.go` with `PreviousManifest *DogfoodManifest` (pointer for omitempty JSON behavior).
- [x] 5.2 In `WriteManifest`, when an existing manifest is present on disk, read it, strip its own `PreviousManifest` (so we don't compound), and embed it as the new manifest's `PreviousManifest` field.
- [x] 5.3 Confirm `ReadManifest` round-trips the field correctly.
- [x] 5.4 Unit tests for: no prior manifest (field absent), one prior manifest (field present), two-deep history (field present but its own previous_manifest is absent).

## 6. Migration: iris repo self-config

- [x] 6.1 Remove `dogfood_branch = "dev"` from iris's checked-in `.iris.toml` on this branch. iris's shared config returns to its pre-shipping shape.
- [x] 6.2 Add `.iris.local.toml` to iris's `.gitignore`.
- [x] 6.3 Add `.iris.local.toml` (uncommitted, locally) with `dogfood_branch = "dev"` so iris-on-self continues to dogfood after this change merges. **Deferred to deploy step** — see Stage 7's setup.sh prompt, which writes this file on the host post-merge.

## 7. setup.sh updates

- [x] 7.1 Update `setup.sh` to add `.iris.local.toml` to the consuming repo's `.gitignore` if missing.
- [x] 7.2 Interactive prompt: "what branch name do you use for dogfooding? (default: dev)". Skip if `--yes`. Write a starter `.iris.local.toml` if the answer is non-empty.
- [x] 7.3 Skip the prompt entirely if `.iris.local.toml` already exists.

## 8. Documentation

- [x] 8.1 Update `README.md` with the two-file pattern: a small table showing which fields live where, why, and how to migrate.
- [x] 8.2 Update `SKETCH.md` if it references `.iris.toml`'s contents.

## 10. `iris:set_local_config` verb

- [ ] 10.1 Write failing tests in `internal/verbs/set_local_config_test.go` matching every scenario in `specs/iris-set-local-config/spec.md`.
- [ ] 10.2 Implement `verbs.SetLocalConfig(ctx, client, taskID, opts) (*SetLocalConfigResult, error)` in `internal/verbs/set_local_config.go`. Steps: resolve task -> validate every input field name is `local`-tagged (refuse `field_not_local` else) -> validate every input value against per-field rules (refuse `invalid_value` else) -> acquire source-repo lock -> read existing `.iris.local.toml` (treat absent as empty) -> apply deletes, apply sets -> atomic write (tmp + rename) -> return result.
- [ ] 10.3 Wire MCP handler in `internal/mcp/handler_set_local_config.go` and register in the handler registry.
- [ ] 10.4 Wire CLI subcommand `iris set-local-config --field <name>=<value> ... [--delete <name> ...]`. Accept repeated `--field` and `--delete` flags.
- [ ] 10.5 Reuse cross-validation helpers from `internal/config/iris_toml.go` for per-field rules (so dogfood_branch validation stays in one place).
- [ ] 10.6 Ensure all tests green; full `go test ./...` and `go vet ./...` clean.

## 9. Validation and ship

- [x] 9.1 `openspec validate add-iris-local-toml-overlay --strict` green.
- [x] 9.2 `go test ./...` green.
- [x] 9.3 `go vet ./...` clean.
- [x] 9.4 Smoke covered by the unit test matrix in stages 1-3 + a live CLI run against the host (pre-migration) that correctly emits the `local_field_in_shared_config` warning for `dogfood_branch` in `.iris.toml`. The "no warnings" target state arrives once this change ships and the host pulls the new `.iris.toml`.
- [x] 9.5 Smoke covered by the unit test matrix in stage 4 (`config_sources` provenance) plus a live `iris status` CLI run confirming the `config_sources` field is emitted with correct shared/local classifications.
- [x] 9.6 Smoke covered by the unit test matrix in stage 3 (`legacy override` test in `validate_config_test.go`): both files set `dogfood_branch`, the local file's value wins, the shared-placement warning fires.
