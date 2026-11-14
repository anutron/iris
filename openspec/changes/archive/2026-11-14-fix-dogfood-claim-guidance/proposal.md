## Why

The `iris_dogfood_claim` grant message and several lease notices still tell agents to deploy with `iris_set_dogfood`. The one-step `iris_dogfood_deploy` is the intended path; `iris_set_dogfood` is the lower-level verb for a caller who already composed a finished SHA.

## What changes

- Grant messages, grant notices, override and recompose-skipped notices, and the claim/set_dogfood tool descriptions point at `iris_dogfood_deploy` (`compose=true`, poll `iris_run_result` for a run id), then `iris_dogfood_release`.
- `iris_set_dogfood` stays documented as the lower-level verb.
- Wording only: no behavior change.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `iris-dogfood-lease`: Require deploy guidance in agent-facing lease messages to name `iris_dogfood_deploy`.

## Impact

Lease result messages and notices, MCP tool descriptions, CLI help, the Iris skill, and README.
