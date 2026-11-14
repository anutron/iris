## ADDED Requirements

### Requirement: One-step dogfood deployment

Iris SHALL expose `iris_dogfood_deploy(task_id, ref, compose, force, lease_override, wait_seconds)` and `iris dogfood-deploy`. It SHALL resolve the target, require a configured dogfood branch and clean host tree, resolve ref to a commit, and deploy that commit on the dogfood branch. Direct deployment SHALL refuse dropping existing dogfood commits unless force is explicit. Composition SHALL merge the requested commit with `--no-ff` onto the current dogfood tip under the source lock, abort conflicts, and preserve the prior dogfood tip on conflict. Composition SHALL preserve existing manifest base/layers and append the requested layer when a prior manifest exists. Without a prior manifest, the generated record SHALL describe the prior dogfood tip and added layer. Composition SHALL preserve existing manifest base/layers and append the requested layer when a prior manifest exists. Without a prior manifest, the generated record SHALL describe the prior dogfood tip and added layer. The source lock SHALL span composition, ref movement, build, restart and readiness. The host SHALL remain on dogfood after deployment.

#### Scenario: Deploy a composed ref

- **WHEN** the lease holder deploys a reachable descendant ref
- **THEN** build and restart execute with dogfood checked out, the host stays there, and the result includes the deployed SHA, readiness outcome and warnings

#### Scenario: Preserve existing work by composition

- **WHEN** compose is true and ref diverges from dogfood
- **THEN** iris merges ref onto dogfood with --no-ff and deploys the resulting merge commit

#### Scenario: Conflict aborts safely

- **WHEN** composition conflicts
- **THEN** iris aborts the merge, retains the prior dogfood tip, and does not build or restart

### Requirement: Configured readiness with evidence

Shared-kind `[dogfood_ready]` configuration SHALL support ports, health_urls, timeout_seconds and interval_seconds. Ports MUST be in 1–65535, URLs MUST be HTTP or HTTPS with a host, and timing values MUST be non-negative (zero uses the default of 120 seconds for timeout and 1 second for interval). Timing values SHALL be at most 86400 seconds and the interval MUST be finite. After restart iris SHALL poll localhost TCP ports and HTTP URLs until all pass together or the configured timeout expires. HTTP readiness SHALL require 2xx without following redirects. A dogfood branch checked out in another worktree SHALL be refused without resetting that worktree. A timeout SHALL fail with one line naming expected versus found checks plus evidence. Results SHALL include a post-cleanup dogfood_lease snapshot for successful deployments, exposing the lease holder and effective expiry supplied by the lease view, along with readiness evidence and the built commit; served_commit SHALL only be populated on successful cross-restart readiness. Without readiness checks, iris SHALL warn that service readiness is unverified and SHALL not claim a served commit.

#### Scenario: Wait for delayed readiness

- **WHEN** one configured port opens before another health URL returns 2xx
- **THEN** deploy waits until both pass before reporting readiness success

#### Scenario: Readiness timeout

- **WHEN** a configured check remains unavailable
- **THEN** deploy returns a failure containing expected and found checks and their latest evidence

#### Scenario: Invalid readiness config

- **WHEN** iris_validate_config reads an invalid port, URL, or timing value
- **THEN** it returns structured field validation errors
