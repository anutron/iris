# Verified spec gaps — 2026-11-13

## Verified gaps

None in the affected pre-archive scope after review fixes.

## Would resolve on archive

The pending iris-dogfood-lease delta covers content-aware claims, persisted requests, reconciliation and notices. The pending iris-set-dogfood delta corrects the override queue promise. See the promise matrix in `index.md`.

## Verified false positives

None; no missing-symbol claims were emitted.

## Resolved during delivery

- Pre-handoff operational Git errors now surface in release warnings and status diagnostics.
- Override-deploy queue wording now permits satisfaction of contained queued requests while preserving ownership and remaining order.

## Scope limits

This is an affected-scope diagnostic audit, not full-project coverage. Pre-existing unrelated behavior was excluded, and inventory intentionally omits commit_sha.
