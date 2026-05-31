## 1. Tests

- [x] 1.1 `set_dogfood` succeeds when the default branch has NO `.iris.toml`, `.iris.local.toml` declares `dogfood_branch=dev`, and the composed SHA carries `.iris.toml` (dev created/pointed, reload runs against dev, source restored to default).
- [x] 1.2 `set_dogfood` still refuses when `dogfood_branch` is set neither in the overlay nor `.iris.local.toml`.
- [x] 1.3 Existing `.iris.toml`-present dogfood tests still pass (overlay resolution unchanged).

## 2. Implementation

- [x] 2.1 `config.PeekLocalDogfoodBranch(repoRoot)`: lenient read of `dogfood_branch` from `.iris.local.toml`; "" on any problem.
- [x] 2.2 `set_dogfood`: when the overlay yields no `dogfood_branch`, fall back to `PeekLocalDogfoodBranch` before refusing.

## 3. Docs

- [x] 3.1 `config-authoring.md` bootstrap runbook: note that `set_dogfood` reads `dogfood_branch` from `.iris.local.toml` even on a bare default branch (no pre-checkout of dev needed).

## 4. Validation

- [x] 4.1 `gofmt`, `go vet`, `go test ./...` green.
- [x] 4.2 `openspec validate set-dogfood-bootstrap-local-read --strict` and `openspec validate --all --strict` clean.
