## 1. Field taxonomy

- [ ] 1.1 Add a `kind` (or equivalent) tag to each field in the `IrisToml` struct, classifying it as `shared` or `local`. Define an exhaustive helper enumerating which fields are which.
- [ ] 1.2 Mark `dogfood_branch` and `ship_ci_timeout_seconds` as `local`; everything else (`schema_version`, `default_branch`, `[build]`, `[restart]`, `[pre_flight]`, `[verify]`, `[post_merge]`) as `shared`.
- [ ] 1.3 Unit-test the taxonomy helper: every field is classified; no field is unclassified.

## 2. Overlay loader

- [ ] 2.1 Add a loader that reads `.iris.toml` then `.iris.local.toml` (if present) and merges them. Absence of the local file is silent.
- [ ] 2.2 Implement merge semantics: for `local`-tagged fields, the local file's value wins; for `shared`-tagged fields, the shared file's value wins and the local file's value is recorded as a `shared_field_in_local_config` warning.
- [ ] 2.3 Track per-field provenance during the merge so it can be exposed via `iris:status`.
- [ ] 2.4 Unit tests for: local-only field overlay, shared-and-local same field (local wins + no warning), shared-only field with local-file override (shared wins + warning), missing local file (silent), malformed local file (structured error).

## 3. Validation updates

- [ ] 3.1 In `iris:validate_config`, run the overlay loader and surface its warnings. Emit `local_field_in_shared_config` per local-tagged field present in `.iris.toml` with a migration hint pointing at `.iris.local.toml`.
- [ ] 3.2 Emit `shared_field_in_local_config` per shared-tagged field present in `.iris.local.toml`.
- [ ] 3.3 Malformed `.iris.local.toml` surfaces a structured error (file + line/column when available) without losing `.iris.toml`'s fields.
- [ ] 3.4 Update `validate_config_test.go` with golden cases for every spec scenario in `specs/iris-validate-config/spec.md`.

## 4. Status integration

- [ ] 4.1 Extend `verbs.Status` to return `config_sources` keyed by top-level field name (`"shared"` or `"local"`).
- [ ] 4.2 Omit fields that were unset in both files (don't emit `"none"`).
- [ ] 4.3 When neither file exists, `config_sources` is an empty object (`{}`), not absent.
- [ ] 4.4 Update `status_test.go` per the scenarios in `specs/iris-status/spec.md`.

## 5. Manifest `previous_manifest`

- [ ] 5.1 Extend `DogfoodManifest` struct in `internal/verbs/dogfood_manifest.go` with `PreviousManifest *DogfoodManifest` (pointer for omitempty JSON behavior).
- [ ] 5.2 In `WriteManifest`, when an existing manifest is present on disk, read it, strip its own `PreviousManifest` (so we don't compound), and embed it as the new manifest's `PreviousManifest` field.
- [ ] 5.3 Confirm `ReadManifest` round-trips the field correctly.
- [ ] 5.4 Unit tests for: no prior manifest (field absent), one prior manifest (field present), two-deep history (field present but its own previous_manifest is absent).

## 6. Migration: iris repo self-config

- [ ] 6.1 Remove `dogfood_branch = "dev"` from iris's checked-in `.iris.toml` on this branch. iris's shared config returns to its pre-shipping shape.
- [ ] 6.2 Add `.iris.local.toml` to iris's `.gitignore`.
- [ ] 6.3 Add `.iris.local.toml` (uncommitted, locally) with `dogfood_branch = "dev"` so iris-on-self continues to dogfood after this change merges.

## 7. setup.sh updates

- [ ] 7.1 Update `setup.sh` to add `.iris.local.toml` to the consuming repo's `.gitignore` if missing.
- [ ] 7.2 Interactive prompt: "what branch name do you use for dogfooding? (default: dev)". Skip if `--yes`. Write a starter `.iris.local.toml` if the answer is non-empty.
- [ ] 7.3 Skip the prompt entirely if `.iris.local.toml` already exists.

## 8. Documentation

- [ ] 8.1 Update `README.md` with the two-file pattern: a small table showing which fields live where, why, and how to migrate.
- [ ] 8.2 Update `SKETCH.md` if it references `.iris.toml`'s contents.

## 9. Validation and ship

- [ ] 9.1 `openspec validate add-iris-local-toml-overlay --strict` green.
- [ ] 9.2 `go test ./...` green.
- [ ] 9.3 `go vet ./...` clean.
- [ ] 9.4 Smoke: `iris validate-config` against the iris repo (self) with `.iris.local.toml` absent — passes, no warnings (since dogfood_branch was removed from shared in 6.1).
- [ ] 9.5 Smoke: create `.iris.local.toml` with `dogfood_branch = "dev"`, `iris validate-config` again — passes, no warnings, `iris:status` reports `config_sources.dogfood_branch = "local"`.
- [ ] 9.6 Smoke: temporarily put `dogfood_branch = "shared"` in `.iris.toml` AND `dogfood_branch = "local"` in `.iris.local.toml` — `iris validate-config` produces the `local_field_in_shared_config` warning, resolved value is `"local"`. Then revert.
