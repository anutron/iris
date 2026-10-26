## Why

Push and fetch can continue after the caller's 30-second deadline, leaving an agent unable to distinguish an ongoing transfer from failure. Part A is approved: return a pollable handle before that deadline while preserving completed results and classified errors.

## What changes

- Run MCP push/fetch through the shared run registry with a bounded wait and polling via `iris_run_result`.
- Preserve inline success payloads and timeout/auth/network/other failure classification.
- Describe restart recovery and polling in tool and agent documentation.
- Add approved Part B: opt-in HTTPS GitHub authentication through the existing secrets resolver and service-account bootstrap, with credentials only in subprocess memory and fail-closed errors.

## Capabilities

### New capabilities

- `iris-git-transfer-runs`: Bounded MCP responses, deduplication, polling and restart semantics for push/fetch.
- `iris-git-auth`: Local per-repo opt-in authentication for unattended push/fetch.

### Modified capabilities

None. The synchronous CLI and transfer verbs retain their existing contracts.

## Impact

MCP push/fetch handlers, shared run registry wiring, transfer command authentication, config schema, secret-source resolution, tool descriptions, agent skill and tests. No real credential or global git/SSH configuration changes.
