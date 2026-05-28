# Implementation tasks: add-branch-cherry-pick-checkout-verbs

**Design doc:** `openspec/changes/add-branch-cherry-pick-checkout-verbs/design.md`

## 1. Failing tests

- [ ] 1.1 Write failing tests for `internal/verbs/branch_create_test.go` covering every scenario in `specs/iris-branch-create/spec.md`. Use real git against tempdirs with a bare origin.
- [ ] 1.2 Write failing tests for `internal/verbs/cherry_pick_test.go` covering `specs/iris-cherry-pick/spec.md`.
- [ ] 1.3 Write failing tests for `internal/verbs/checkout_test.go` covering `specs/iris-checkout/spec.md`, including dirty-tree + in-progress-merge recovery cases.
- [ ] 1.4 Confirm every `it should X` acceptance criterion in `design.md` has a corresponding failing test (Prove-It Pattern).

## 2. `iris:branch_create`

**Depends on:** Stage 1

- [ ] 2.1 Implement `internal/verbs/branch_create.go`: `func BranchCreate(ctx context.Context, in BranchCreateInput) (*BranchCreateResult, error)`
- [ ] 2.2 Input validation: empty + leading-dash refusal for `name` and `base_ref`; `git check-ref-format --branch` for the name
- [ ] 2.3 Source-repo resolution + allowlist + lock
- [ ] 2.4 Default-branch refusal (`name` == resolved default OR `main` OR `master`)
- [ ] 2.5 Existing-branch refusal via `git rev-parse --verify refs/heads/<name>`
- [ ] 2.6 Shell out: `git branch <name> <base_ref>`
- [ ] 2.7 Read created SHA via `git rev-parse refs/heads/<name>`
- [ ] 2.8 Verify Stage 1.1 tests pass

## 3. `iris:cherry_pick`

**Depends on:** Stage 1

- [ ] 3.1 Implement `internal/verbs/cherry_pick.go`: `func CherryPick(ctx context.Context, in CherryPickInput) (*CherryPickResult, error)`
- [ ] 3.2 Input validation: empty + leading-dash refusal for `commit` and `target_branch`
- [ ] 3.3 Source-repo resolution + allowlist + lock
- [ ] 3.4 Default-branch refusal on `target_branch`
- [ ] 3.5 Existence checks: `git rev-parse --verify refs/heads/<target_branch>` and `git rev-parse --verify <commit>^{commit}`
- [ ] 3.6 Shell out: `git checkout <target_branch>` then `git cherry-pick <commit>`
- [ ] 3.7 On non-zero cherry-pick exit: capture conflict paths from `git diff --name-only --diff-filter=U`, then `git cherry-pick --abort`, return structured error
- [ ] 3.8 On success: read new HEAD via `git rev-parse HEAD`, return `{ cherry_picked: true, commit, target_branch, new_sha }`
- [ ] 3.9 Verify Stage 1.2 tests pass

## 4. `iris:checkout`

**Depends on:** Stage 1

- [ ] 4.1 Implement `internal/verbs/checkout.go`: `func Checkout(ctx context.Context, in CheckoutInput) (*CheckoutResult, error)`
- [ ] 4.2 Input validation: empty + leading-dash refusal for `branch`
- [ ] 4.3 Source-repo resolution + allowlist + lock
- [ ] 4.4 Capture `prior_branch` and `prior_head` before any state change
- [ ] 4.5 When `force=true`: best-effort `merge --abort`, `cherry-pick --abort`, `rebase --abort`, then `git checkout -f <branch>`
- [ ] 4.6 When `force=false`: `git checkout <branch>` (propagate git's refusal for dirty tree / in-progress op)
- [ ] 4.7 Return `{ checked_out: true, branch, head_sha, prior_branch, prior_head }`
- [ ] 4.8 Verify Stage 1.3 tests pass

## 5. MCP handlers, Cobra subcommands, daemon registration

**Depends on:** Stages 2, 3, 4

- [ ] 5.1 Implement `internal/mcp/handler_branch_create.go`, `handler_cherry_pick.go`, `handler_checkout.go`
- [ ] 5.2 Implement `cmd/iris/branch_create.go`, `cherry_pick.go`, `checkout.go`
- [ ] 5.3 Register the 3 new tools in `internal/daemon/run.go`'s `toolDefinitions()` and `RegisterHandler` block
- [ ] 5.4 Update SKETCH.md "future verbs" list (remove the three entries this change implements)
- [ ] 5.5 Update README.md CLI section
- [ ] 5.6 Run `make test` under `-race`; verify all stages pass
- [ ] 5.7 Run `openspec validate add-branch-cherry-pick-checkout-verbs --strict`
