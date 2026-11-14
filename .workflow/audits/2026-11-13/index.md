# Spec audit — 2026-11-13

Scoped pre-archive audit for `coalesce-dogfood-claims`. Production files changed since `9f9ca06` and affected callers were checked against relevant base specs plus the two pending deltas. Current review fixes are included. This is diagnostic coverage of affected behavior; it is not a full-project audit and records no `commit_sha`.

## Summary

- Modules analyzed: 5; production files enumerated: 9.
- Deterministically enumerated branch records: 752.
- Assessed branch records: 612; covered: 522; implementation detail: 90.
- Unchanged unrelated branch records excluded: 140 (daemon lifecycle and ship workflow outside the recompose caller).
- Behavioral gaps verified: 0; contradictions: 0; unimplemented affected promises: 0; false positives suppressed: 0.
- OpenSpec strict validation: 38 of 38 items pass (29 base capabilities and 9 changes); informational long-requirement notices only.
- Code-to-spec and spec-to-code were inspected separately. No missing-symbol findings required verification.

## Coverage by module

| Module | Enumerated | Assessed | Covered | Internal | Excluded | Gaps |
|---|---:|---:|---:|---:|---:|---:|
| cli | 38 | 38 | 35 | 3 | 0 | 0 |
| daemon | 28 | 0 | 0 | 0 | 28 | 0 |
| mcp | 53 | 53 | 53 | 0 | 0 | 0 |
| lease | 431 | 431 | 354 | 77 | 0 | 0 |
| deployment | 202 | 90 | 80 | 10 | 112 | 0 |

The deterministic helper records conditionals and early returns separately; these counts describe records, not independent feature scenarios. The daemon schema change is declarative and receives an explicit schema review despite introducing no conditional branch.

## Would resolve on archive

Pending coverage comes from:

- `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md`: claim containment, pinning, persisted requests, deployed notices, invalidation and queue reconciliation.
- `openspec/changes/coalesce-dogfood-claims/specs/iris-set-dogfood/spec.md`: override keeps ownership while satisfying contained queued requests.

Pending MODIFIED requirements supersede corresponding base sections for this audit. They are not reported as current base coverage. The added set-dogfood delta resolves the stale base promise that an override leaves the entire queue unchanged.

## Independent spec to code check

| Affected promise | Implementation | Evidence |
|---|---|---|
| Claim API, SHA validation and pinning | resolveClaimSHA; DogfoodClaim; CLI newDogfoodClaimCmd; MCP NewDogfoodClaimHandler; daemon toolDefinitions | Refs resolve once before withLease; option-like/unresolvable refs stop before lease mutation. CLI/MCP/schema carry SHA. |
| Existing holder retains ownership; containment precedes queue/grant | leaseMachine.Claim; claimResult | Holder branch calls contact before containment. Other callers get already_deployed with no grant/check-in/contact. Queued request removal clears empty-queue warning. |
| Retain or replace queued request; prompt waiting completion | leaseMachine.Claim; DogfoodClaim poll closure | Omission inherits queued SHA; explicit SHA replaces it; poll closure carries inherited SHA after queue removal. 250ms polling stops on grant or already_deployed. |
| Persist request and deployed notice metadata | LeaseEntry; LeaseNotice; marshalLease; saveLease; readLeaseFile; RecoverLeaseAfterRestart | Optional SHA remains backward compatible; JSON roundtrip preserves request and notices; atomic rename persists state. Restart retains requests while restoring timer windows. |
| Satisfy all contained requests on successful deploy and pre-handoff | SetDogfood; ReconcileQueue; grantNext | Reconciliation follows successful Reload; failed reload exits before it. grantNext reconciles before task liveness/grant. Remaining queue order and holder survive successful-deploy reconciliation. Legacy entries have no containment match. |
| Warnings preserve success and unproven requests | leaseContainment; withLease; DogfoodRelease; ReadDogfoodLeaseView; SetDogfood | Ancestry exit 1 means absent; operational errors warn, never satisfy. Release surfaces warnings and timer handoffs retain status containment diagnostics. Reconciliation persistence errors append warnings after successful deployment. |
| Deployed notice delivery and obsolete notice cancellation | ReconcileQueue; notify; cancelNotices; CancelInvalidDeployments; deliverLeaseEffects | Deployed notices include SHA, repo, branch, generation, time, confirmation tool, and rewrite caveat. All notices persist delivery IDs; cancels execute before posts; rollback cancellation precedes Reload, including failure. Unrelated holder contact preserves deployed notices. |
| Cancel release warning when the last waiter disappears | clearEmptyQueueWarning; Claim; ReconcileQueue; Release | Final/check-in notices and scheduled release are cleared after last-waiter removal while ordinary idle cap remains. |
| Override holder and remaining queue behavior | enterLeaseGate; SetDogfood; notifyOverridden | New iris-set-dogfood delta replaces stale unchanged-queue promise: holder remains, contained requests are satisfied, other waiters keep order. |
| Agent instructions | claude/skills/iris/SKILL.md:87 | Skill asks for SHA, handles already_deployed without ownership, preserves explicit holder release, confirms notices, and limits live to branch contents. |

## Verified gaps

None after the warning propagation fix and cross-capability delta correction.

## Verified false positives

None. No claims that a referenced symbol was absent were emitted. The cross-module symbol index is retained in `symbols.json`.

## Unimplemented spec promises

None in the affected scope. Manifest helpers, reload internals, unrelated CLI verbs and the broader ship flow were considered only at their existing interfaces. The pre-existing ship recompose caller discards successful SetDogfood warnings; that broader caller limitation is outside this change and does not invalidate SetDogfood's warning contract.

## Validation context

Parent reports all Go tests, build and vet passing, with focused race coverage passing. This audit independently ran OpenSpec strict validation and deterministic inventory/symbol/branch helpers; it did not rerun the parent's test suite. PR #24 has no configured GitHub checks.

## Delta from last audit

First audit of this affected scope; no comparable prior result is available. No full-project coverage trend is inferred.
