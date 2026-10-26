## 1. Transfer run tests

- [x] 1.1 Add handler tests for inline push/fetch results, slow transfers and polling, cancellation, classified auth failures, deduplication and registry restart.

## 2. Implementation

- [x] 2.1 Share the run registry with push/fetch, add bounded waits and option-aware identity, and mark source-lock acquisition.
- [x] 2.2 Update polling and restart guidance in tool descriptions, README and the shipped agent skill.

## 3. Verification and delivery

- [x] 3.1 Run focused tests and review the diff with hera-review and the test-adversary lens.
- [ ] 3.2 Obtain coordinator gate permission, run make test, go vet and strict OpenSpec validation, then push and open a PR.
