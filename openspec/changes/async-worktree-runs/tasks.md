## 1. Detached execution in the verbs

- [ ] 1.1 Write tests: caller cancellation doesn't kill a check, an orphaned grandchild doesn't hold the run open, and the ceiling reports `[timeout]`.
- [ ] 1.2 Detach `RunChecks` and `RunBuild` with the 30-minute ceiling and `WaitDelay`.

## 2. Run registry and tools

- [ ] 2.1 Write registry and handler tests: inline fast result, running handle, clamped wait, poll to completion, unknown ID, dedup, expiry, repo on every response.
- [ ] 2.2 Implement `mcp.RunRegistry`, the `wait_seconds` handling in the run_checks and run_build handlers, and `iris_run_result`, and register them in the daemon.

## 3. Documentation and delivery

- [ ] 3.1 Document `wait_seconds` and `iris_run_result` in the tool descriptions and the iris skill.
- [ ] 3.2 Run `make test`, `go vet ./...` and `openspec validate --all --strict`.
