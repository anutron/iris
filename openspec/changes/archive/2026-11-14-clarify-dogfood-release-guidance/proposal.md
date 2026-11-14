## Why

Agents need explicit guidance to release dogfood when the user is away or a problem needs fixing, so other agents can take their turn.

## What changes

- Add short release guidance to granted claim results, successful deployments, and holder notices.
- Repeat the guidance in dogfood tool descriptions and the shipped Iris skill.
- Keep lease state, queue ordering, and timers unchanged.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `iris-dogfood-lease`: Require plain release guidance across agent-facing dogfood surfaces.

## Impact

Dogfood result messages, lease notices, MCP tool descriptions, the Iris skill, and message coverage tests.
