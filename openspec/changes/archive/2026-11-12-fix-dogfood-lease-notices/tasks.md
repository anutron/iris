## 1. Config

- [x] 1.1 Add `in_flight_max_seconds` (default 900) to `[dogfood_lease]` with validation and tests
## 2. State machine

- [x] 2.1 Tests, then code: persisted notice tracking, cancel on obsolete, generation and timestamp in notices
- [x] 2.2 Tests, then code: in-flight expiry
## 3. Delivery

- [x] 3.1 Notify returns argus state; record it; status warning for unsubmitted check-in
## 4. Skill and CLI

- [x] 4.1 Update iris SKILL.md (confirm before acting on a notice)
- [x] 4.2 Add CLI tests for `iris dogfood *` and `--lease-override`
## 5. Finish

- [x] 5.1 gofmt, build, vet, tests, openspec validate; ralph-review; archive
