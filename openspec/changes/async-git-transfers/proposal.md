## Why

Push and fetch can continue after the caller's 30-second deadline, leaving an agent unable to distinguish an ongoing transfer from failure. Part A is approved: return a pollable handle before that deadline while preserving completed results and classified errors.

## What changes

- Run MCP push/fetch through the shared run registry with a bounded wait and polling via `iris_run_result`.
- Preserve inline success payloads and timeout/auth/network/other failure classification.
- Describe restart recovery and polling in tool and agent documentation.
- Leave unattended credential configuration for the separately approved Part B decision.

## Capabilities

### New capabilities

- `iris-git-transfer-runs`: Bounded MCP responses, deduplication, polling and restart semantics for push/fetch.

### Modified capabilities

None. The synchronous CLI and transfer verbs retain their existing contracts.

## Impact

MCP push/fetch handlers, shared run registry wiring, transfer start notification, tool descriptions, agent skill and handler tests. No credential or global git/SSH configuration changes.
