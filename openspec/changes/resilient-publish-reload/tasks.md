## 1. Regression coverage

- [x] 1.1 Demonstrate caller cancellation skips restart in publish and reload using temporary fixture repositories.
- [x] 1.2 Cover real build/restart failures, build timeout, and persisted outcomes through status.

## 2. Implementation

- [x] 2.1 Detach after the locked cancellation check and bound remaining git operations.
- [x] 2.2 Record explicit restart dispatch outcome in audit entries and document recovery through status.

## 3. Validation and delivery

- [x] 3.1 Run race tests, vet, strict OpenSpec validation, and review including the test adversary lens.
- [x] 3.2 Commit, push, open a non-draft PR, and report tests and URL to the coordinator.

Validation: 18 cancellation cases pass under the race detector; vet and all 32 strict OpenSpec checks pass. One full race run passed; two later full runs exceeded the unchanged push timeout test's 2.5-second timing threshold (2.54s and 2.68s). That test passes alone. The timing failure is documented in PR #15 rather than changing unrelated push behavior.
