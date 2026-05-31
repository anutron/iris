## 1. Specs (spec-first)

- [x] 1.1 `iris-set-dogfood` delta: resolve `dogfood_branch` from the merged overlay; updated refusal hint; build the composed SHA; propagate overlay warnings.
- [x] 1.2 `iris-reload` delta: optional caller-supplied build-branch checkout/restore.
- [x] 1.3 `iris-ship-feature` delta: resolve `ship_ci_timeout_seconds` from the merged overlay.

## 2. Implementation

- [x] 2.1 `reload.go`: add `ReloadInput.BuildBranch`; after the lock, check out the branch for the build and restore the entry branch on every exit path (warn on restore failure).
- [x] 2.2 `set_dogfood.go`: resolve `dogfood_branch` via `config.LoadOverlay`; pass it as `BuildBranch`; propagate overlay warnings; update the refusal hint to name `.iris.local.toml`.
- [x] 2.3 `ship_feature.go`: `shipCITimeout` reads `ship_ci_timeout_seconds` via `config.LoadOverlay`.

## 3. Tests

- [x] 3.1 Fixture: dogfood fixtures commit `.iris.toml` onto the composed SHA's tree (the build now runs against it).
- [x] 3.2 `set_dogfood` honors `dogfood_branch` set only in `.iris.local.toml` (gitignored; tree stays clean).
- [x] 3.3 `set_dogfood` build deploys the composed SHA, not the default branch (marker proves which tree was built); source repo is restored to the default branch afterward.
- [x] 3.4 `ship_feature` honors `ship_ci_timeout_seconds` set only in `.iris.local.toml`.
- [x] 3.5 Existing reload tests unaffected (BuildBranch empty).

## 4. Validation

- [x] 4.1 `gofmt`, `go vet`, `go build ./...`, `go test ./...` green.
- [x] 4.2 `openspec validate fix-dogfood-overlay-and-build --strict` clean.
- [x] 4.3 `openspec validate --all --strict` clean.
