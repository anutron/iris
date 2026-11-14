## 1. Reproduce

- [x] 1.1 Add a synthetic large-log regression exercising check execution, polling, and the 4 MiB proxy decoding limit; observe failure before implementation.

## 2. Fix

- [x] 2.1 Bound check output, persist full logs, and include bounded recognized summaries and failures.
- [x] 2.2 Preserve small-response behavior and surface bounded error results, including timeouts and log-write failures.
- [x] 2.3 Guard encoded callback size and update tool documentation.

## 3. Verify and deliver

- [x] 3.1 Run regression tests, the full race-enabled suite, vet, and strict OpenSpec validation.
- [ ] 3.2 Review the diff, push, open a PR for Aaron, and check CI without merging or deploying.
