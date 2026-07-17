## 1. Specs (spec-first)

- [x] 1.1 `iris-set-dogfood` delta: MODIFY the verb requirement to add the `force` input, the worktree-guarded ref move, and the ancestry refusal; ADD an ancestry-safety requirement with refuse/force scenarios.

## 2. Tests (TDD — write first, watch fail)

- [x] 2.1 Worktree-guard: ref move succeeds when the dogfood branch IS the checked-out branch (rewrite of the prior reset-failure test).
- [x] 2.2 Ancestry: non-descendant `newSHA` refused without `force`; error names the dropped-commit count; no manifest, no ref move.
- [x] 2.3 Ancestry: non-descendant `newSHA` allowed WITH `force`; ref moves; a warning naming the drop is emitted.
- [x] 2.4 Ancestry: descendant `newSHA` proceeds normally (no refusal, no drop warning).
- [x] 2.5 Regression guards stay green: overlay-only `dogfood_branch` resolves; composed-SHA build deploys the dogfood tree.

## 3. Implementation

- [x] 3.1 `set_dogfood.go`: add `SetDogfoodOpts.Force`.
- [x] 3.2 `set_dogfood.go`: ancestry check after reading `previous_sha`, before the manifest write — refuse (naming the count) unless `Force`; on `Force`, append a warning.
- [x] 3.3 `set_dogfood.go`: worktree-guarded ref move — `branch -f` when not checked out, `reset --hard` in the holding worktree otherwise.
- [x] 3.4 `handler_set_dogfood.go`: accept and forward `force`.
- [x] 3.5 `cmd/iris/set_dogfood.go`: `--force` flag.
- [x] 3.6 `internal/daemon/run.go`: `force` in the `iris_set_dogfood` input schema.

## 4. Validation

- [x] 4.1 `gofmt`, `go build ./...`, `go vet ./...`, `go test ./...` green.
- [x] 4.2 `openspec validate harden-set-dogfood-safety --strict` clean.
- [x] 4.3 `openspec validate --all --strict` clean.
