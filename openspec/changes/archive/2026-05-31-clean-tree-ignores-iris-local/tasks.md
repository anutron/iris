## 1. Tests

- [x] 1.1 `checkCleanTree` returns nil when the only uncommitted change is an untracked `.iris.local.toml`.
- [x] 1.2 `checkCleanTree` still errors (dirty) when a real uncommitted change is present alongside `.iris.local.toml`, and the reported paths exclude `.iris.local.toml`.

## 2. Implementation

- [x] 2.1 Add a helper that filters `.iris.local.toml` entries from `git status --porcelain=v1` output.
- [x] 2.2 `checkCleanTree` decides dirty/clean against the filtered output and reports only the non-ignored dirty paths. Both `iris:reload` pre-flight and `iris:publish` inherit this via the shared function.

## 3. Validation

- [x] 3.1 `gofmt`, `go vet`, `go test ./...` green (existing reload/publish dirty-tree tests still pass).
- [x] 3.2 `openspec validate clean-tree-ignores-iris-local --strict` and `openspec validate --all --strict` clean.
