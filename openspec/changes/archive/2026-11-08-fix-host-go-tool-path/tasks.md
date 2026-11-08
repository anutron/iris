## 1. Regression coverage

- [x] 1.1 Add fake-PATH tests for GOBIN, GOPATH, HOME fallback, executable precedence and direct versus descendant lookup.
- [x] 1.2 Add isolated regression tests through configured builds/restarts, hooks, worktree checks/builds, and git/gh subprocesses.

## 2. Shared command environment

- [x] 2.1 Implement bounded background Go directory discovery, startup warming, immediate cold-call fallback, and a shared host command constructor.
- [x] 2.2 Route host subprocess call sites through the constructor, retaining environment overlays and existing process controls.
- [x] 2.3 Document host PATH behavior and investigation evidence.

## 3. Verification and delivery

- [x] 3.1 Run formatting, build, vet, targeted tests, installer tests, and strict OpenSpec validation; record full-suite load failures and interruption.
- [x] 3.2 Review the diff and regression tests, commit, push and open the PR via iris, then report to the coordinator without merging or restarting.
- [x] 3.3 Run the full race gate after the coordinator grants a quiet-host slot; this repo has no CI workflow. Passed `GOFLAGS=-p=2 GOMAXPROCS=4 make test` with race detection; no isolated reruns needed.
