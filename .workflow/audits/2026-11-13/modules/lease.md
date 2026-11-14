# lease spec coverage

## Summary

- enumerated: 431
- total: 431
- covered: 354
- uncovered behavioral: 0
- uncovered implementation: 77
- excluded unchanged: 0
- contradictions: 0
- unimplemented promises: 0

## Deterministic branch classifications

Every enumerated record is listed by source line and helper kind. Records sharing a function, classification and requirement are grouped; counts include early-return records separately.

### internal/verbs/dogfood_lease.go

- **[COVERED]** `leasePath` (3): 42:if, 43:early_return, 45:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

- **[COVERED]** `loadLease` (3): 52:if, 53:early_return, 55:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

- **[COVERED]** `readLeaseFile` (7): 60:if, 61:if, 62:early_return, 64:early_return, 67:if, 68:early_return, 70:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

- **[COVERED]** `marshalLease` (3): 74:if, 77:if, 80:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

- **[COVERED]** `saveLease` (17): 86:if, 87:early_return, 91:if, 92:early_return, 94:if, 95:early_return, 98:if, 99:early_return, 103:if, 106:early_return, 108:if, 111:early_return, 113:if, 115:early_return, 117:if, 119:early_return, 121:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

- **[UNCOVERED-IMPLEMENTATION]** `lockLease` (7): 137:if, 138:if, 139:if, 140:if, 142:else, 148:early_return, 149:if.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `setLeaseDiag` (1): 170:if.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `getLeaseDiag` (3): 180:if, 181:early_return, 183:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `resolveDogfoodConfig` (6): 207:if, 208:early_return, 210:if, 214:if, 217:if, 220:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Configurable lease timers — “Iris SHALL use the defaults for a missing block or a missing key.”

- **[COVERED]** `resolveLeaseRepo` (5): 225:if, 226:early_return, 228:if, 229:early_return, 242:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Configurable lease timers — “Iris SHALL use the defaults for a missing block or a missing key.”

- **[COVERED]** `withLease` (9): 257:if, 258:early_return, 264:if, 265:early_return, 270:if, 275:if, 277:else, 281:early_return, 307:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Lease operations are serialized and evaluated by the daemon — “Every lease read-modify-write SHALL be serialized per source repo across processes”

- **[COVERED]** `withLease` (5): 290:early_return, 297:if, 302:if, 303:if, 304:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Contained queued commits are satisfied without lease handoff — “Before any queued task is granted the lease during a holder handoff, Iris SHALL also re-evaluate queued requested commits and satisfy contained entries instead of granting them.”

- **[UNCOVERED-IMPLEMENTATION]** `leaseFileExists` (3): 312:if, 313:early_return, 316:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `deliverLeaseEffects` (7): 324:if, 325:if, 331:early_return, 338:if, 350:if, 356:if, 365:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Iris messages agents through argus pane delivery — “Caller-influenced text in messages (repo, branch, reason, caller name, SHA) SHALL have control characters replaced and SHALL be truncated to 120 characters”

- **[UNCOVERED-IMPLEMENTATION]** `leaseStateFiles` (3): 373:if, 374:early_return, 377:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `ActiveLeaseRepos` (2): 387:if, 392:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `TickLease` (3): 398:if, 399:early_return, 402:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Lease operations are serialized and evaluated by the daemon — “Every lease read-modify-write SHALL be serialized per source repo across processes”

- **[COVERED]** `StartupScanLeases` (2): 410:if, 414:if.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Lease operations are serialized and evaluated by the daemon — “Every lease read-modify-write SHALL be serialized per source repo across processes”

- **[UNCOVERED-IMPLEMENTATION]** `RunLeaseScheduler` (6): 425:if, 433:when, 434:early_return, 435:when, 437:if, 438:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `leaseTargetRepo` (5): 478:if, 479:early_return, 482:if, 483:early_return, 485:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

- **[UNCOVERED-IMPLEMENTATION]** `taskNameOf` (3): 489:if, 490:early_return, 492:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `DogfoodClaim` (17): 498:if, 499:early_return, 501:if, 502:early_return, 505:if, 506:early_return, 511:if, 512:early_return, 523:early_return, 525:if, 526:early_return, 531:when, 532:early_return, 533:when, 535:if, 536:early_return, 539:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `clampLeaseWait` (5): 544:if, 545:early_return, 547:if, 548:early_return, 550:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[UNCOVERED-IMPLEMENTATION]** `dedupe` (2): 557:if, 562:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `claimResult` (8): 574:if, 577:if, 580:if, 583:elsif, 583:else, 590:else, 593:if, 599:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `DogfoodRenew` (9): 611:if, 612:early_return, 615:if, 616:early_return, 620:if, 624:early_return, 626:if, 627:early_return, 629:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_renew` verb — “When the caller holds the lease, iris SHALL record contact and return”

- **[COVERED]** `DogfoodRelease` (7): 642:if, 643:early_return, 646:if, 647:early_return, 651:if, 652:early_return, 654:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_release` verb — “When the caller holds the lease, iris SHALL release it and grant it to the next live queued task, if any.”

- **[COVERED]** `DogfoodForceRelease` (10): 675:if, 676:early_return, 679:if, 680:early_return, 685:if, 690:if, 691:early_return, 693:if, 696:if, 700:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_force_release` verb — “Force-release SHALL succeed even when argus is unreachable; messaging is then skipped and a warning is returned.”

- **[COVERED]** `forceReleaseTarget` (17): 709:if, 711:if, 712:early_return, 715:if, 716:early_return, 720:case, 721:when, 722:if, 725:when, 727:if, 733:if, 734:if, 735:early_return, 737:early_return, 740:if, 741:early_return, 743:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_force_release` verb — “Force-release SHALL succeed even when argus is unreachable; messaging is then skipped and a warning is returned.”

- **[UNCOVERED-IMPLEMENTATION]** `leaseNames` (5): 747:if, 748:early_return, 751:if, 752:early_return, 755:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `DogfoodQueueMove` (10): 773:if, 774:early_return, 776:if, 780:if, 781:early_return, 785:if, 786:early_return, 788:if, 789:early_return, 791:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_queue_move` verb — “Iris SHALL move the task, shift the others to keep their relative order, record the move in the lease history”

- **[COVERED]** `newLeaseGateError` (3): 811:if, 814:if, 818:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-set-dogfood/spec.md` § Dogfood lease gate — “leave the lease holder unchanged; after successful reload, satisfy contained queued commit requests according to the dogfood lease rules, preserving all other waiters and their order.”

- **[COVERED]** `enterLeaseGate` (11): 840:case, 841:when, 844:when, 846:when, 847:if, 851:else, 858:if, 859:early_return, 861:if, 862:early_return, 864:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-set-dogfood/spec.md` § Dogfood lease gate — “leave the lease holder unchanged; after successful reload, satisfy contained queued commit requests according to the dogfood lease rules, preserving all other waiters and their order.”

- **[COVERED]** `finish` (2): 870:if, 871:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-set-dogfood/spec.md` § Dogfood lease gate — “leave the lease holder unchanged; after successful reload, satisfy contained queued commit requests according to the dogfood lease rules, preserving all other waiters and their order.”

- **[COVERED]** `notifyOverridden` (4): 878:if, 879:early_return, 882:if, 887:if.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Iris messages agents through argus pane delivery — “Caller-influenced text in messages (repo, branch, reason, caller name, SHA) SHALL have control characters replaced and SHALL be truncated to 120 characters”

- **[COVERED]** `leaseHolderRef` (5): 898:if, 899:early_return, 901:if, 902:early_return, 904:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

- **[COVERED]** `noticeHolder` (3): 910:if, 911:early_return, 914:if.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

- **[COVERED]** `ReadDogfoodLeaseView` (11): 965:if, 966:early_return, 981:if, 984:early_return, 986:if, 992:if, 995:if, 999:if, 1010:if, 1014:if, 1017:if.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Notice delivery state is recorded — “When a check-in is accepted but not submitted, iris SHALL still start the check-in clock, and `iris_status` SHALL include a warning”

- **[COVERED]** `ReadDogfoodLeaseView` (2): 1020:if, 1023:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Contained queued commits are satisfied without lease handoff — “Before any queued task is granted the lease during a holder handoff, Iris SHALL also re-evaluate queued requested commits and satisfy contained entries instead of granting them.”

### internal/verbs/dogfood_lease_content.go

- **[COVERED]** `resolveClaimSHA` (7): 13:if, 14:early_return, 16:if, 17:early_return, 20:if, 21:early_return, 23:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[COVERED]** `leaseContainment` (13): 37:early_return, 38:if, 39:early_return, 41:if, 44:if, 48:if, 51:else, 56:if, 58:if, 60:else, 62:if, 67:if, 71:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

### internal/verbs/dogfood_lease_state.go

- **[UNCOVERED-IMPLEMENTATION]** `holderText` (3): 144:if, 145:early_return, 147:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `Error` (3): 154:if, 157:if, 160:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `refList` (1): 168:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `newLeaseMachine` (1): 200:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

- **[COVERED]** `pruneNotices` (2): 208:if, 212:if.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Obsolete lease notices are cancelled — “A cancel SHALL be issued before any new notice is posted in the same operation.”

- **[COVERED]** `expireStaleFlight` (5): 224:if, 225:early_return, 228:if, 231:if, 232:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Stale in-flight marks expire — “Clearing it SHALL be recorded in the lease history, and the holder SHALL then be subject to ordinary silence, dead and idle releases.”

- **[COVERED]** `cancelNotices` (4): 244:if, 253:if, 255:if, 262:if.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Obsolete lease notices are cancelled — “A cancel SHALL be issued before any new notice is posted in the same operation.”

- **[UNCOVERED-IMPLEMENTATION]** `cancelled` (3): 272:if, 273:early_return, 276:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `forgetNotice` (1): 283:if.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `cleanText` (3): 303:if, 310:if, 313:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Iris messages agents through argus pane delivery — “Caller-influenced text in messages (repo, branch, reason, caller name, SHA) SHALL have control characters replaced and SHALL be truncated to 120 characters”

- **[UNCOVERED-IMPLEMENTATION]** `fmtLeaseDur` (3): 317:if, 318:early_return, 320:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `slugChars` (3): 326:if, 328:else, 332:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `deliveryID` (4): 337:if, 343:if, 346:if, 350:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `notify` (2): 355:if, 362:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Iris messages agents through argus pane delivery — “Caller-influenced text in messages (repo, branch, reason, caller name, SHA) SHALL have control characters replaced and SHALL be truncated to 120 characters”

- **[COVERED]** `history` (1): 372:if.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

- **[UNCOVERED-IMPLEMENTATION]** `queueIndex` (3): 379:if, 380:early_return, 383:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `refs` (1): 391:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `holderRef` (3): 397:if, 398:early_return, 400:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `Refusal` (4): 408:if, 412:if, 415:else, 418:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_renew` verb — “When the caller holds the lease, iris SHALL record contact and return”

- **[UNCOVERED-IMPLEMENTATION]** `nextCheckinAt` (3): 422:if, 423:early_return, 425:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `queued` (1): 430:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `Claim` (24): 439:if, 442:if, 448:if, 449:if, 453:early_return, 455:if, 456:if, 462:early_return, 464:if, 466:early_return, 468:if, 470:if, 473:if, 477:early_return, 480:if, 483:if, 485:if, 487:early_return, 494:if, 497:else, 503:if, 508:if, 509:early_return, 511:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_claim` verb — “Otherwise, if the configured dogfood branch HEAD contains the requested commit (equal to it or a descendant), iris SHALL return `already_deployed` before granting or queueing.”

- **[UNCOVERED-IMPLEMENTATION]** `granted` (1): 515:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `moveTo` (2): 523:if, 526:if.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_queue_move` verb — “Iris SHALL move the task, shift the others to keep their relative order, record the move in the lease history”

- **[COVERED]** `QueueMove` (5): 537:if, 538:if, 539:early_return, 541:early_return, 545:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_queue_move` verb — “Iris SHALL move the task, shift the others to keep their relative order, record the move in the lease history”

- **[COVERED]** `Renew` (3): 550:if, 551:early_return, 555:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_renew` verb — “When the caller holds the lease, iris SHALL record contact and return”

- **[COVERED]** `Release` (6): 561:if, 564:if, 567:early_return, 569:if, 574:early_return, 576:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_release` verb — “When the caller holds the lease, iris SHALL release it and grant it to the next live queued task, if any.”

- **[COVERED]** `ForceRelease` (5): 581:if, 582:early_return, 585:if, 586:early_return, 588:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_force_release` verb — “Force-release SHALL succeed even when argus is unreachable; messaging is then skipped and a warning is returned.”

- **[COVERED]** `BeginFlight` (4): 594:if, 595:early_return, 598:if, 602:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Warmer check-ins and contact — “Granting the lease SHALL also count as contact.”

- **[COVERED]** `EndFlight` (4): 607:if, 608:early_return, 610:if, 613:if.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Warmer check-ins and contact — “Granting the lease SHALL also count as contact.”

- **[COVERED]** `Touch` (1): 622:if.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Warmer check-ins and contact — “Granting the lease SHALL also count as contact.”

- **[COVERED]** `MarkUndelivered` (7): 632:if, 633:early_return, 636:if, 637:early_return, 640:case, 641:when, 644:when.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

- **[COVERED]** `NoteDeliveryState` (1): 653:if.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Iris messages agents through argus pane delivery — “Caller-influenced text in messages (repo, branch, reason, caller name, SHA) SHALL have control characters replaced and SHALL be truncated to 120 characters”

- **[COVERED]** `ReleaseDead` (1): 661:if.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Liveness consulted before releasing on silence — “A dead queued task SHALL be dropped from the queue and skipped.”

- **[UNCOVERED-IMPLEMENTATION]** `containsStr` (3): 668:if, 669:early_return, 672:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `removeStr` (2): 678:if, 682:early_return.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[UNCOVERED-IMPLEMENTATION]** `noteName` (4): 688:if, 689:early_return, 691:if, 695:if.

  Internal formatting, lookup, locking, or bookkeeping; no additional externally visible contract.

- **[COVERED]** `contact` (2): 704:if, 705:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Warmer check-ins and contact — “Granting the lease SHALL also count as contact.”

- **[COVERED]** `release` (3): 731:if, 732:early_return, 737:if.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § `iris:dogfood_release` verb — “When the caller holds the lease, iris SHALL release it and grant it to the next live queued task, if any.”

- **[COVERED]** `grantNext` (3): 752:if, 758:if, 762:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Contained queued commits are satisfied without lease handoff — “Before any queued task is granted the lease during a holder handoff, Iris SHALL also re-evaluate queued requested commits and satisfy contained entries instead of granting them.”

- **[COVERED]** `contains` (3): 767:if, 768:early_return, 771:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Contained queued commits are satisfied without lease handoff — “Before any queued task is granted the lease during a holder handoff, Iris SHALL also re-evaluate queued requested commits and satisfy contained entries instead of granting them.”

- **[COVERED]** `ReconcileQueue` (1): 779:if.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Contained queued commits are satisfied without lease handoff — “Before any queued task is granted the lease during a holder handoff, Iris SHALL also re-evaluate queued requested commits and satisfy contained entries instead of granting them.”

- **[COVERED]** `CancelInvalidDeployments` (2): 796:if, 800:if.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Obsolete lease notices are cancelled — “A cancel SHALL be issued before any new notice is posted in the same operation.”

- **[COVERED]** `clearEmptyQueueWarning` (2): 809:if, 810:early_return.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Obsolete lease notices are cancelled — “A cancel SHALL be issued before any new notice is posted in the same operation.”

- **[COVERED]** `Tick` (7): 821:if, 822:if, 825:early_return, 827:if, 828:early_return, 834:if, 836:early_return.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Idle release cap — “When `idle_release_seconds` have passed since the holder's last contact, iris SHALL release the lease whether or not anyone is queued”

- **[COVERED]** `Tick` (17): 840:if, 841:if, 842:early_return, 844:if, 851:early_return, 855:case, 856:when, 858:when, 861:early_return, 865:if, 866:case, 867:when, 870:case, 871:when, 873:when, 881:early_return, 882:when.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Release on silence only when someone is waiting — “When the queue is empty, a missed check-in SHALL NOT release the lease, and no final warning SHALL be sent.”

- **[COVERED]** `Tick` (5): 885:early_return, 890:if, 893:if, 895:early_return, 898:if.

  Spec: `openspec/specs/iris-dogfood-lease/spec.md` § Warmer check-ins and contact — “A new task joining the queue SHALL trigger an immediate check-in on the holder, unless one is already outstanding.”

- **[COVERED]** `RecoverLeaseAfterRestart` (5): 913:if, 914:early_return, 918:if, 919:if, 923:if.

  Spec: `openspec/changes/coalesce-dogfood-claims/specs/iris-dogfood-lease/spec.md` § Lease state persists across iris restarts — “the ordered queue, including each entry's optional full resolved requested `sha`; a missing field SHALL preserve legacy task-only behavior”

## Spec to code verification

The independent affected-promise matrix is in `../index.md`. All affected promises have implementation; pending requirements supersede matching base sections.

## Unimplemented spec promises

None in affected scope.

## Verified false positives

None. No missing-symbol claims were emitted.
