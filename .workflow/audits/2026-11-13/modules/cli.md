# cli spec coverage

## Summary

- enumerated: 38
- total: 38
- covered: 35
- uncovered behavioral: 0
- uncovered implementation: 3
- excluded unchanged: 0
- contradictions: 0
- unimplemented promises: 0

## Deterministic branch classifications

Every enumerated record is listed by source line and helper kind. Records sharing a function, classification and requirement are grouped; counts include early-return records separately.

### cmd/iris/dogfood.go

- **[UNCOVERED-IMPLEMENTATION]** `printJSON` (3): 16:if, 17:early_return, 20:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `newDogfoodCmd` (1): 43:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `newDogfoodClaimCmd` (6): 55:if, 56:early_return, 59:if, 60:early_return, 62:early_return, 70:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `newDogfoodRenewCmd` (6): 81:if, 82:early_return, 85:if, 86:early_return, 88:early_return, 93:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_renew` verb — “When the caller holds the lease, iris SHALL record contact and return”

- **[COVERED]** `newDogfoodReleaseCmd` (6): 104:if, 105:early_return, 108:if, 109:early_return, 111:early_return, 116:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_release` verb — “When the caller holds the lease, iris SHALL release it and grant it to the next live queued task, if any.”

- **[COVERED]** `newDogfoodForceReleaseCmd` (6): 130:if, 131:early_return, 136:if, 137:early_return, 139:early_return, 146:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_force_release` verb — “Force-release SHALL succeed even when argus is unreachable; messaging is then skipped and a warning is returned.”

- **[COVERED]** `newDogfoodQueueMoveCmd` (10): 160:if, 161:early_return, 163:if, 164:early_return, 167:if, 168:early_return, 171:if, 172:early_return, 174:early_return, 179:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_queue_move` verb — “Iris SHALL move the task, shift the others to keep their relative order, record the move in the lease history”

## Spec to code verification

The independent affected-promise matrix is in `../index.md`. All affected promises have implementation; pending requirements supersede matching base sections.

## Unimplemented spec promises

None in affected scope.

## Verified false positives

None. No missing-symbol claims were emitted.
