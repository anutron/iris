## Why

Dogfood deploys currently require a checkout/reload race that can restart on main and serve mismatched assets. Long builds also lose their response at the transport deadline even though deployment continues.

## What changes

- Add one-step dogfood deployment with optional merge composition and readiness polling.
- Leave the host on dogfood throughout build, restart, and verification.
- Return pollable run handles for deploy, set_dogfood, and reload.
- Add shared `[dogfood_ready]` configuration and agent guidance.

## Capabilities

### New capabilities

- `iris-dogfood-deploy`: Lease-gated deployment, optional composition, and readiness evidence.

### Modified capabilities

- `iris-set-dogfood`: Keep dogfood checked out after deploying.
- `iris-reload`: Allow dogfood entry, preserve its checkout, and verify readiness.
- `iris-dogfood-lease`: Protect the entire deployment as in-flight work.
- `iris-worktree-runs`: Bounded asynchronous responses for host deployment operations.

## Impact

Go verbs, MCP handlers/catalog, CLI, config validation/taxonomy, README and agent instructions. No live host operations are used in tests.
