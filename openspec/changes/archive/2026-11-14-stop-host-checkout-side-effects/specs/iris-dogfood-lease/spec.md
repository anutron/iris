## ADDED Requirements

### Requirement: Host-mover lease gate

Verbs that deliberately move or modify the host's working branch SHALL change what dogfood serves only when the lease is free, held by the caller, or `lease_override=true` was passed. "Changes what dogfood serves" means: the host is on the configured `dogfood_branch` and the verb would leave it, or the verb would modify the `dogfood_branch` ref. The refusal SHALL be a lease gate error carrying the holder. An override SHALL add a `LEASE OVERRIDE:` warning as the first warning and queue an override notice to the holder. Repos without `dogfood_branch` are exempt. Verbs with side effects that are not their purpose (`iris:gh_pr_merge`) SHALL instead never move the host and have no override.

#### Scenario: Gate is skipped without a dogfood branch

- **WHEN** the repo configures no `dogfood_branch`
- **THEN** host-mover verbs never consult the lease

#### Scenario: Free lease passes

- **WHEN** the host is on `dev` and nobody holds the lease
- **THEN** a host-mover verb proceeds
