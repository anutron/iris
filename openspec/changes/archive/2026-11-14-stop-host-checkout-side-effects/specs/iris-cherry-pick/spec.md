## ADDED Requirements

### Requirement: cherry_pick consults the dogfood lease

`iris:cherry_pick` SHALL accept `lease_override` (boolean, default false) and apply the dogfood lease gate when the pick would change what dogfood serves: the host is on `dogfood_branch` (the verb would move it) or `target_branch` is the `dogfood_branch`. A non-holder SHALL be refused before any git mutation unless `lease_override=true`, which warns first and notifies the holder.

#### Scenario: Pick onto the dogfood branch by a non-holder is refused

- **WHEN** task A cherry-picks onto `dev` while task B holds the lease
- **THEN** the verb errors naming B and `dev` is unchanged

#### Scenario: Pick onto an unrelated branch while the host is off dogfood

- **WHEN** the host is on a feature branch and the target is another feature branch
- **THEN** the gate does not apply
