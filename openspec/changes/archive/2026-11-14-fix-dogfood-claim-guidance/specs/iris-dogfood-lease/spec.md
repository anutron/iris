## ADDED Requirements

### Requirement: Lease guidance names the one-step deploy

Agent-facing lease guidance that tells a holder how to deploy SHALL name `iris_dogfood_deploy` (with `compose=true` to merge onto the current dogfood tip, and `iris_run_result` polling when a run id is returned), followed by `iris_dogfood_release`. It SHALL NOT direct callers to `iris_set_dogfood` as the deploy step; `iris_set_dogfood` SHALL remain documented as the lower-level verb for a caller who already composed a finished SHA. This change SHALL affect wording only.

#### Scenario: Claim is granted

- **WHEN** `iris_dogfood_claim` returns granted
- **THEN** its message tells the caller to call `iris_dogfood_deploy`, then `iris_dogfood_release`
- **AND** it does not tell the caller to call `iris_set_dogfood`

#### Scenario: Holder receives a lease notice

- **WHEN** iris sends a grant, override, host-move or recompose-skipped notice to the holder
- **THEN** the notice names `iris_dogfood_deploy` rather than `iris_set_dogfood` as the way to deploy
