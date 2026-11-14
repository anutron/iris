## Why

Forgotten leases and sessions waiting on permission prompts block the shared dogfood host. Coordinators need clear access to the next merge window.

## What changes

- Add a configurable 30-minute absolute hold cap that renewals cannot extend.
- Treat needs_input as silent even when busy, and check ended tasks every tick.
- Expose deadlines, queue estimates and best-effort Argus task metadata.
- Document and test next-slot reservation using existing position 1 without a SHA.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `iris-dogfood-lease`: bounded holds, prompt-aware release and visibility.

## Impact

Lease machinery, local timer configuration, Argus metadata client and shipped skill/snippet docs. Gate and flight signatures stay stable. No real dogfood host or state is touched.
