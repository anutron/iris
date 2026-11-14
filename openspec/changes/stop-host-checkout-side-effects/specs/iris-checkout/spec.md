## ADDED Requirements

### Requirement: checkout consults the dogfood lease

`iris:checkout` SHALL accept `lease_override` (boolean, default false). When the repo has a `dogfood_branch`, the host is on it, the requested branch differs, and another task holds the dogfood lease, the verb SHALL refuse with a lease error naming the holder and SHALL NOT change the checkout, unless `lease_override=true`, in which case it SHALL proceed, put a `LEASE OVERRIDE:` warning first in `warnings`, and notify the holder. A free lease, a lease held by the caller, a checkout of the dogfood branch itself, or a repo without `dogfood_branch` SHALL pass unchanged.

#### Scenario: Non-holder refused when leaving the dogfood branch

- **WHEN** the host is on `dev`, task B holds the lease, and task A checks out `main`
- **THEN** the verb errors naming B and the host stays on `dev`

#### Scenario: Override proceeds with a warning

- **WHEN** the same call passes `lease_override=true`
- **THEN** the host moves to `main`, `warnings[0]` starts with `LEASE OVERRIDE:`, and B is notified

#### Scenario: Holder may move

- **WHEN** the caller holds the lease
- **THEN** the checkout proceeds with no lease warning
