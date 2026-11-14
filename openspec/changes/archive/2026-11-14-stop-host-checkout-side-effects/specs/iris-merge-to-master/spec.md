## ADDED Requirements

### Requirement: merge_to_master and complete_task consult the dogfood lease

Because merging requires the default branch checked out, `iris:merge_to_master` (real and dry-run) and `iris:complete_task` SHALL apply the dogfood lease gate when the host is on `dogfood_branch`: a non-holder is refused before any git mutation. `iris:merge_to_master` accepts `lease_override` (boolean, default false), which warns first and notifies the holder. `iris:complete_task` has no override. Hosts not on the dogfood branch, free leases, and holder callers pass.

#### Scenario: Non-holder merge refused while host serves dogfood

- **WHEN** the host is on `dev`, task B holds the lease, and task A calls merge_to_master
- **THEN** the verb errors naming B and the host stays on `dev`

#### Scenario: complete_task refused the same way

- **WHEN** the same condition holds for complete_task
- **THEN** it returns the lease error with no checkpoints reached
