## ADDED Requirements

### Requirement: Deploy lease gate and flight lifecycle

Dogfood deploy SHALL reuse the set_dogfood lease gate before any composition, ref movement, manifest write, build or restart. A non-holder SHALL receive the same actionable LeaseGateError unless lease_override is explicit. The holder SHALL be marked in flight throughout composition, build, restart and readiness; every success and failure SHALL end flight. Override SHALL warn and notify the holder without taking its lease.

#### Scenario: Non-holder refused

- **WHEN** a non-holder invokes dogfood deploy without override
- **THEN** no deploy mutation occurs and the response names the holder and claim action

#### Scenario: Readiness keeps holder in flight

- **WHEN** a deploy is waiting for readiness
- **THEN** the holder remains in flight until readiness succeeds or fails
