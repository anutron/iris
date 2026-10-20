# Git-transfer timeout tests are flaky under full-suite load

- **Found:** 2026-10-20, in the coordinator's review of PR #16 (not caused by that PR's diff). It was also seen in PR #15 (2.54 s and 2.68 s).
- **Tests:** `TestPush_*` and `TestFetch_ConfiguredGitTransferTimeoutClassifiesAsTimeout` in `internal/verbs/push_test.go` and `fetch_test.go`.
- **What:** they configure a 1 s `git_transfer_timeout_seconds` and assert the verb returns within a tight wall-clock budget (about 2.5 s). Under `make test` (`-race`, the whole package in parallel), the measured time reached 2.6 s, so the run failed even though the timeout classification was correct. They pass when run alone.
- **Why it matters:** a red `make test` that is really a timing artefact trains people to rerun until it's green, which hides real failures.
- **Likely fix:** assert the `[timeout]` classification and a generous upper bound, for example under 10 s, which still proves the slow remote's full duration wasn't awaited. Alternatively, make these tests non-parallel. Don't tighten the production timeout.
- **Status:** not fixed. The coordinator asked for a note only.
