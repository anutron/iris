# deployment spec coverage

## Summary

- enumerated: 202
- total: 90
- covered: 80
- uncovered behavioral: 0
- uncovered implementation: 10
- excluded unchanged: 112
- contradictions: 0
- unimplemented promises: 0

## Deterministic branch classifications

Every enumerated record is listed by source line and helper kind. Records sharing a function, classification and requirement are grouped; counts include early-return records separately.

### internal/verbs/set_dogfood.go

- **[COVERED]** `SetDogfood` (44): 73:if, 74:early_return, 76:if, 77:early_return, 79:if, 80:early_return, 85:if, 86:early_return, 97:if, 98:early_return, 101:if, 104:if, 113:if, 114:early_return, 121:if, 123:else, 130:if, 131:early_return, 140:if, 141:early_return, 144:if, 145:early_return, 153:if, 162:if, 173:if, 174:if, 176:if, 177:early_return, 189:if, 190:early_return, 192:if, 193:early_return, 205:if, 206:if, 207:early_return, 209:else, 211:if, 212:early_return, 214:if, 215:if, 216:early_return, 218:else, 219:if, 220:early_return.

  Spec: `openspec/specs/iris-set-dogfood/spec.md` § `iris:set_dogfood` verb — “The reload the verb triggers SHALL build and restart against the composed SHA”

- **[COVERED]** `SetDogfood` (9): 229:if, 240:if, 249:if, 250:early_return, 256:if, 259:if, 264:if, 271:if, 275:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Contained queued commits are satisfied without lease handoff — “Before any queued task is granted the lease during a holder handoff, Iris SHALL also re-evaluate queued requested commits and satisfy contained entries instead of granting them.”

- **[UNCOVERED-IMPLEMENTATION]** `worktreeForBranch` (7): 292:if, 293:early_return, 299:case, 300:when, 302:when, 303:early_return, 306:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `droppedCommitCount` (3): 314:if, 315:early_return, 317:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

### internal/verbs/ship_feature.go

- **[OUT-OF-SCOPE]** `ShipFeature` (52): 126:if, 127:early_return, 129:if, 130:early_return, 135:case, 136:when, 138:when, 140:if, 141:early_return, 144:early_return, 149:if, 150:early_return, 154:if, 155:early_return, 157:if, 158:early_return, 162:if, 163:early_return, 167:if, 170:if, 171:early_return, 173:early_return, 175:early_return, 182:if, 184:if, 185:early_return, 189:if, 190:early_return, 194:if, 195:early_return, 199:if, 200:early_return, 215:if, 216:early_return, 222:if, 223:early_return, 225:if, 227:early_return, 238:if, 242:if, 243:early_return, 245:if, 246:early_return, 249:if, 250:early_return, 256:if, 257:early_return, 258:else, 278:if, 283:else, 285:if, 290:early_return.

  Unchanged behavior outside the affected call path; excluded from coverage denominator.

- **[OUT-OF-SCOPE]** `shipCITimeout` (3): 302:if, 305:if, 308:early_return.

  Unchanged behavior outside the affected call path; excluded from coverage denominator.

- **[OUT-OF-SCOPE]** `waitForChecks` (13): 330:if, 331:early_return, 333:if, 335:early_return, 338:if, 339:early_return, 344:if, 345:early_return, 347:if, 348:early_return, 354:when, 355:early_return, 356:when.

  Unchanged behavior outside the affected call path; excluded from coverage denominator.

- **[OUT-OF-SCOPE]** `queryCheckRuns` (5): 368:if, 369:early_return, 375:if, 376:early_return, 378:early_return.

  Unchanged behavior outside the affected call path; excluded from coverage denominator.

- **[OUT-OF-SCOPE]** `summarizeCheckRuns` (4): 386:if, 390:case, 391:when, 397:early_return.

  Unchanged behavior outside the affected call path; excluded from coverage denominator.

- **[OUT-OF-SCOPE]** `approvePRForShip` (3): 404:if, 405:early_return, 407:early_return.

  Unchanged behavior outside the affected call path; excluded from coverage denominator.

- **[OUT-OF-SCOPE]** `mergePRForShip` (3): 414:if, 415:early_return, 417:early_return.

  Unchanged behavior outside the affected call path; excluded from coverage denominator.

- **[OUT-OF-SCOPE]** `readMergeCommitSHA` (5): 426:if, 427:early_return, 434:if, 435:early_return, 437:early_return.

  Unchanged behavior outside the affected call path; excluded from coverage denominator.

- **[OUT-OF-SCOPE]** `createPRForBranch` (10): 450:if, 457:if, 458:early_return, 462:if, 463:early_return, 466:if, 467:early_return, 470:if, 471:early_return, 473:early_return.

  Unchanged behavior outside the affected call path; excluded from coverage denominator.

- **[COVERED]** `recomposeAfterShip` (27): 500:if, 501:early_return, 504:if, 505:early_return, 507:if, 509:early_return, 515:if, 520:if, 523:early_return, 533:if, 534:early_return, 536:if, 538:if, 544:early_return, 558:if, 559:early_return, 562:if, 563:early_return, 570:if, 571:early_return, 573:if, 576:early_return, 594:if, 604:if, 605:early_return, 611:early_return, 614:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Contained queued commits are satisfied without lease handoff — “Before any queued task is granted the lease during a holder handoff, Iris SHALL also re-evaluate queued requested commits and satisfy contained entries instead of granting them.”

- **[OUT-OF-SCOPE]** `replayLayered` (14): 627:if, 628:early_return, 637:if, 638:early_return, 642:if, 643:early_return, 645:if, 646:early_return, 648:if, 652:if, 655:early_return, 660:if, 661:early_return, 663:early_return.

  Unchanged behavior outside the affected call path; excluded from coverage denominator.

## Spec to code verification

The independent affected-promise matrix is in `../index.md`. All affected promises have implementation; pending requirements supersede matching base sections.

## Unimplemented spec promises

None in affected scope.

## Verified false positives

None. No missing-symbol claims were emitted.
