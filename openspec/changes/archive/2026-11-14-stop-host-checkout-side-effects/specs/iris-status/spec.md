## ADDED Requirements

### Requirement: status reports host branch versus dogfood branch

`iris:status` SHALL report `dogfood_branch` (empty when not configured) and `host_off_dogfood` (true when `dogfood_branch` is set, the host is on a named branch, and that branch differs from it). When true it SHALL append a warning naming both branches.

#### Scenario: Host on a different branch

- **WHEN** `dogfood_branch = "dev"` and the host is on `main`
- **THEN** `host_off_dogfood` is true and a warning names `main` and `dev`

#### Scenario: Host on the dogfood branch

- **WHEN** the host is on `dev`
- **THEN** `host_off_dogfood` is false and no such warning appears
