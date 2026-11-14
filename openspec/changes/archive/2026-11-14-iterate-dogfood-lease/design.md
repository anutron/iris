## Context

The classifier prioritizes busy over needs_input. Terminal tasks are only checked when check-ins fall due. Idle expiry already handles absent contact but repeated renewals can monopolize the host. Existing position 1 supports next-slot reservations; omitting SHA avoids commit coalescing.

## Goals / Non-goals

- Bound forgotten holds and renewals, retaining bounded in-flight deployment protection.
- Release completed, archived and stopped tasks promptly.
- Reuse position 1; no new priority kind, heartbeat protocol or Argus repository changes.

## Decisions

- Default max_hold_seconds to 1800. Contact and restart retain GrantedAt. Evaluate the cap before claims, renewal and gate entry, on ticks and after EndFlight. A valid in-flight mark defers release until completion or its existing cap.
- Consult liveness every tick. NeedsInput takes precedence over Busy and uses existing two grace windows when waiters exist.
- Expose expires_at and last_contact_at. Queue estimated_by values bound each slot assuming current configuration/order and allow for flight deferral. They are estimates, not scheduled grants.
- Best-effort PUT compact per-repo summaries to Argus task metadata after changes, including clearing departed participants. Metadata is advisory; failures cannot block lease operations.

## Risks / Trade-offs

- Long testing sessions must requeue → surface expiry in results and notices.
- Additional liveness reads → reuse per-operation cache and existing timeouts.
- Queue reorder/config edits invalidate estimates → label estimates explicitly.

## Migration plan

Existing grant timestamps apply immediately, including across restarts. Missing configuration defaults to 30 minutes. No persisted-state migration is required.
