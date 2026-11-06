## 1. Tests first

- [ ] 1.1 Add a `worktree_run` test with a fixture script that backgrounds a nested grandchild writing its PID, shrinks `worktreeRunTimeout`, and asserts the run returns `[timeout]` and every PID is gone
- [ ] 1.2 Add a `run_checks` test that args reach the script as separate argv entries, with spaces and shell metacharacters verbatim
- [ ] 1.3 Add `run_checks` validation tests for leading `-`, NUL, empty arg, 65 args, a 513-byte arg, and the inclusive 64 x 512 boundary, each asserting nothing runs
- [ ] 1.4 Add handler tests for `args` in the input schema, validation before `runs.Start`, and registry key behavior (same args join, different args or order do not, `check="a"`+`["b"]` vs `check="a b"` do not)

## 2. Process-group kill

- [ ] 2.1 In `runWorktreeCommand` set `SysProcAttr{Setpgid: true}` and a `cmd.Cancel` that SIGKILLs `-pid`, ignoring `ESRCH`, mirroring `secrets/resolver.go` and `reload.go`
- [ ] 2.2 Confirm the existing detach, lock-wait and orphan-grandchild tests still pass, for both `run_checks` and `run_build`

## 3. `args` support

- [ ] 3.1 Add `RunChecksOptions.Args`, the validator with its limit constants, and argv and `command` display construction in `internal/verbs/run_checks.go`
- [ ] 3.2 Add `args` to the handler input, call the validator before `runs.Start`, and build the registry key from check and args in `internal/mcp/handler_run_checks.go`
- [ ] 3.3 Register `args` (array of strings) in the tool schema in `internal/daemon/run.go`
- [ ] 3.4 Add the repeatable CLI flag to `cmd/iris/run_checks.go`

## 4. Docs and verification

- [ ] 4.1 Document `args`, its limits and the ceiling's process-group kill in `claude/skills/iris/SKILL.md` and `README.md`
- [ ] 4.2 Run `go test ./...`, `go vet ./...` and `openspec validate --all --strict`
