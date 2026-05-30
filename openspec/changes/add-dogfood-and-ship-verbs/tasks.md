## 1. Config schema

- [x] 1.1 Add `DogfoodBranch string` field to `IrisToml` in `internal/config/iris_toml.go` with TOML tag `dogfood_branch` and `omitempty` JSON tag.
- [x] 1.2 Add `ShipCITimeoutSeconds int` field with TOML tag `ship_ci_timeout_seconds`, defaulting to 600 when unset.
- [x] 1.3 Extend the existing cross-validator to enforce: dogfood_branch is a valid git ref name, dogfood_branch != default_branch, ship_ci_timeout_seconds >= 0.
- [x] 1.4 Update `iris_toml_test.go` and `config_test.go` with golden cases per the `iris-validate-config` spec scenarios.

## 2. Manifest storage

- [x] 2.1 Define `DogfoodManifest` struct in `internal/verbs/dogfood_manifest.go` with `Base{Ref,SHA}`, `Layered[]LayeredEntry`, `Note`, `RecordedAt`.
- [x] 2.2 Implement `WriteManifest(stateDir string, m *DogfoodManifest) error` — writes to `dogfood-manifest.json` atomically (tmp + rename).
- [x] 2.3 Implement `ReadManifest(stateDir string) (*DogfoodManifest, error)` — returns nil-no-error when file absent.
- [x] 2.4 Unit tests for write/read round-trip, atomic-write behavior on simulated mid-write failure, and the recorded_at stamping.

## 3. `iris:set_dogfood` verb

- [x] 3.1 Write failing tests in `internal/verbs/set_dogfood_test.go` matching every scenario in `specs/iris-set-dogfood/spec.md`.
- [x] 3.2 Implement `verbs.SetDogfood(ctx, client, taskID, opts) (*SetDogfoodResult, error)` in `internal/verbs/set_dogfood.go`. Steps: resolve task -> require dogfood_branch config -> verify SHA reachable -> acquire source-repo lock -> write manifest -> hard-reset (create branch if missing) -> reload sequence -> return result.
- [x] 3.3 Wire MCP handler in `internal/mcp/handler_set_dogfood.go` and register in the handler registry.
- [x] 3.4 Wire CLI subcommand `iris set-dogfood --sha <sha> --manifest <path-or-json>` in `cmd/`.
- [x] 3.5 Ensure all tests green; update integration tests for the manifest-write-before-reset ordering.

## 4. Status integration

- [ ] 4.1 Add failing tests in `internal/verbs/status_test.go` for the three manifest scenarios (present, absent, malformed).
- [ ] 4.2 Extend `verbs.Status` to read the manifest via `ReadManifest` and populate the new `Dogfood` field. Append a warning on parse failure.
- [ ] 4.3 Confirm no new side effects (existing "no side effects" scenario still passes).

## 5. `iris:ship_feature` verb — pr mode

- [ ] 5.1 Write failing tests in `internal/verbs/ship_feature_test.go` for the `via: "pr"` scenarios (push + open PR, refuse default branch, refuse missing branch, refuse unknown via).
- [ ] 5.2 Implement `verbs.ShipFeature` for `via: "pr"` only: resolve task -> validate branch != default -> push branch -> open PR via existing `verbs.GhPrCreate` plumbing -> return.
- [ ] 5.3 Wire MCP handler in `internal/mcp/handler_ship_feature.go`.
- [ ] 5.4 Wire CLI subcommand.

## 6. `iris:ship_feature` verb — pr-auto mode

- [ ] 6.1 Add failing tests for `via: "pr-auto"` happy path and the CI-failure, CI-timeout, and no-checks scenarios. Use a fake GitHub client (extend the existing test harness) for check status.
- [ ] 6.2 Implement check-waiting: poll PR's combined status / check runs every N seconds up to `ship_ci_timeout_seconds`. Skip wait when zero checks are configured.
- [ ] 6.3 On checks passing, approve the PR (via GitHub API) then merge using the configured `merge_method`.
- [ ] 6.4 Run `iris_fetch` semantics on the source repo after merge.

## 7. Post-ship dogfood re-compose

- [ ] 7.1 Add failing tests for the re-compose scenarios: shipped feature present in manifest (drops + re-applies), not in manifest (skip), no manifest at all (skip), conflict during re-apply (preserve prior dogfood state).
- [ ] 7.2 Implement `recomposeAfterShip(ctx, source, shippedBranch) (*RecomposeResult, error)`: read manifest -> if shippedBranch in `layered`, drop -> fetch new base -> re-apply remaining `layered` entries via cherry-pick in a scratch worktree or branch -> on success, call into the shared `set_dogfood` core to atomically update branch + manifest -> on conflict, return structured error without mutating dogfood state.
- [ ] 7.3 Ensure the conflict path leaves the dogfood branch and manifest untouched (verified by post-test assertions on SHA and file mtime).

## 8. Documentation and self-config

- [ ] 8.1 Update `README.md` with the new verbs and the `dogfood_branch` config field.
- [ ] 8.2 Update `.iris.toml` at the repo root to set `dogfood_branch = "dev"` so iris itself uses the new feature.
- [ ] 8.3 Add a short section to `SKETCH.md` or equivalent on the origin-first model and the dogfood/ship motions.

## 9. Final validation

- [ ] 9.1 Run `openspec validate add-dogfood-and-ship-verbs --strict`. Resolve any failures.
- [ ] 9.2 Run the full test suite (`make test` or `go test ./...`). All green.
- [ ] 9.3 Run `iris validate-config` against the iris repo itself with the new config. Confirm pass.
- [ ] 9.4 Manual smoke: create a scratch branch with a commit, call `iris set-dogfood` with its SHA + a manifest, confirm dev branch resets and service reloads.
- [ ] 9.5 Manual smoke: `iris ship-feature --via pr` on a throwaway branch in a personal repo, confirm PR opens.
