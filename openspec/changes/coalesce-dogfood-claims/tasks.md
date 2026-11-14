## 1. Approval

- [x] 1.1 Review proposal and delta with Aaron in Plannotator; wait for explicit approval before tests or code. Approved in session 59cabc92-9f3f-4f20-926c-eb2ec62db955.

## 2. Tests from the delta

- [x] 2.1 Add failing state-machine tests for containment before grants, holder reclaims, queued SHA replacement/retention, all-waiter satisfaction, handoffs and last-waiter warning cancellation.
- [x] 2.2 Add failing verb tests with real Git ancestry for host ref resolution, missing branches, invalid inputs, persisted requests, wait polling, successful/failed deployment reconciliation and operational errors.
- [x] 2.3 Add failing notice tests for cancellation order, restart persistence, release/reclaim after satisfaction and forced rollback invalidation.
- [x] 2.4 Add failing CLI and MCP tests for optional SHA input and already_deployed output while preserving task-only behavior.

## 3. Implementation

- [x] 3.1 Add optional persisted requested SHA and deployed-notice SHA, commit resolution and ancestry checks in the I/O shell.
- [x] 3.2 Implement already_deployed claim outcomes, queue satisfaction, history, last-waiter cleanup and pre-handoff checks without changing legacy behavior.
- [x] 3.3 Reconcile after successful set_dogfood reload, cancel invalidated notices on branch movement, and surface reconciliation warnings.
- [x] 3.4 Wire CLI --sha and MCP schema/handler input; update the Iris skill with claim and notice handling.

## 4. Validation and delivery

- [x] 4.1 Run focused tests, go test ./..., and openspec validate --all --strict before committing. Also passed go build ./..., go vet ./..., and focused race tests.
- [ ] 4.2 Push via iris_push and open a PR via iris_gh_pr_create; resolve CI failures until green.
- [ ] 4.3 Run ralph-review and spec-audit --pre-archive, address findings, and repeat relevant checks.
- [ ] 4.4 Archive the change in the same PR as its final commit, validate, push, confirm CI green and report PR/checks to the coordinator.
