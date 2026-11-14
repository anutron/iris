# mcp spec coverage

## Summary

- enumerated: 53
- total: 53
- covered: 53
- uncovered behavioral: 0
- uncovered implementation: 0
- excluded unchanged: 0
- contradictions: 0
- unimplemented promises: 0

## Deterministic branch classifications

Every enumerated record is listed by source line and helper kind. Records sharing a function, classification and requirement are grouped; counts include early-return records separately.

### internal/mcp/handler_dogfood_lease.go

- **[COVERED]** `leaseErr` (5): 17:if, 18:early_return, 23:if, 24:early_return, 26:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `leaseJSON` (3): 31:if, 32:early_return, 34:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `decodeInput` (4): 38:if, 39:if, 41:early_return, 44:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `NewDogfoodClaimHandler` (6): 56:early_return, 58:if, 59:early_return, 62:if, 63:early_return, 65:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `NewDogfoodRenewHandler` (6): 75:early_return, 77:if, 78:early_return, 81:if, 82:early_return, 84:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `NewDogfoodReleaseHandler` (6): 90:early_return, 92:if, 93:early_return, 96:if, 97:early_return, 99:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `NewDogfoodForceReleaseHandler` (6): 111:early_return, 113:if, 114:early_return, 117:if, 118:early_return, 120:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `NewDogfoodQueueMoveHandler` (6): 133:early_return, 135:if, 136:early_return, 139:if, 140:early_return, 142:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

### internal/mcp/handler_set_dogfood.go

- **[COVERED]** `NewSetDogfoodHandler` (11): 25:early_return, 27:if, 28:if, 29:early_return, 32:if, 33:early_return, 41:if, 42:early_return, 45:if, 46:early_return, 48:early_return.

  Spec: `openspec/specs/iris-set-dogfood/spec.md` § `iris:set_dogfood` verb — “The reload the verb triggers SHALL build and restart against the composed SHA”

## Spec to code verification

The independent affected-promise matrix is in `../index.md`. All affected promises have implementation; pending requirements supersede matching base sections.

## Unimplemented spec promises

None in affected scope.

## Verified false positives

None. No missing-symbol claims were emitted.
