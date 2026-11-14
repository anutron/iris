## 1. Lease bounds and lifecycle

- [x] 1.1 Write failing tests for cap, renewal, flight protection, lifecycle and prompt liveness.
- [x] 1.2 Implement validated local timer and bounded machine behavior with stable gate signatures.

## 2. Visibility and workflow

- [x] 2.1 Test and expose deadlines, queue estimates and best-effort task metadata.
- [x] 2.2 Test and document coordinator next-slot reservation and release guidance.

## 3. Validation and delivery

- [x] 3.1 Run make test vet fmt build and review changes.
- [x] 3.2 Archive change and validate all specs strictly.

## Verification results

- Focused lease regression tests passed with race detection, including expiry, prompt liveness, ended tasks, gate refusal, visibility and metadata.
- make vet fmt build passed.
- make test was run; the final full run timed out after ten minutes in three unrelated MergeToMaster git-process tests. Every other package passed. Earlier full runs also hit short-deadline process-output flakes; the isolated proxy test passed. No broad retry is claimed green.
- Tests used only pure machines and temporary repositories/state. No real Sketch host or lease state was touched.

- The default max_hold_seconds was adjusted to 1800 (30 minutes), including notice/status/renewal expiry expectations and documentation. The focused lease/config regression suite passed with race detection and a 180-second ceiling; vet, fmt, build and strict OpenSpec validation passed again.
