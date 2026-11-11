## ADDED Requirements

### Requirement: Dogfood lease gate

`iris:set_dogfood` SHALL refuse a caller that does not hold the source repo's dogfood lease (see `iris-dogfood-lease`), unless the new `lease_override` input is set. The caller is the `task_id` input. A call with no `task_id` (CLI or iris-on-iris default) has no lease identity and SHALL be treated as not holding the lease. The gate SHALL run after `dogfood_branch` resolution and SHA reachability, and before the ancestry check, the manifest write, any git mutation and any reload, so a refusal has no side effects.

The refusal error SHALL name the current holder (task ID and name) or say that no one holds the lease, give the caller's queue position if it is queued, and point the caller to `iris_dogfood_claim`, or to `lease_override` to bypass the lease.

New input:

- `lease_override` (bool, optional, default `false`): deploy even though the caller does not hold the dogfood lease. It is a DISTINCT input from `force`: `lease_override` relaxes ONLY the lease gate, and `force` relaxes ONLY the commit-dropping ancestry refusal. Neither implies the other.

When `lease_override` lets a non-holder through, iris SHALL:

- proceed with the deploy;
- put a warning beginning `LEASE OVERRIDE:` FIRST in `warnings`, naming the holder or "no holder";
- message the current holder, if there is one and it is not the caller, that its dogfood tree was replaced, naming the caller and the new SHA;
- leave the lease holder and queue unchanged.

When the caller holds the lease, the call SHALL count as lease contact at its start and again when it finishes, whether it succeeds or fails. While the call is running, the holder SHALL NOT be released by any silence or idle timer; force-release can still release it.

The CLI SHALL expose the input as `--lease-override`, and the MCP tool schema SHALL describe the lease requirement and both override inputs.

#### Scenario: Non-holder is refused without side effects

- **GIVEN** task `A` holds the dogfood lease and task `B` is queued at position 1
- **WHEN** `B` calls `iris_set_dogfood` with a reachable SHA and a well-formed manifest and no `lease_override`
- **THEN** iris returns an error naming `A` as holder, `B`'s position 1, and `iris_dogfood_claim` / `lease_override`
- **AND** iris writes no manifest, moves no ref, and runs no reload

#### Scenario: Free lease still requires a claim

- **GIVEN** no task holds the dogfood lease
- **WHEN** task `A` calls `iris_set_dogfood` without first calling `iris_dogfood_claim` and without `lease_override`
- **THEN** iris refuses with an error saying no one holds the lease and pointing to `iris_dogfood_claim`

#### Scenario: Holder deploys normally and the call counts as contact

- **GIVEN** task `A` holds the dogfood lease
- **WHEN** `A` calls `iris_set_dogfood` with a descendant SHA
- **THEN** the deploy proceeds exactly as before this change, with no lease warning, and `A`'s warmer clock restarts

#### Scenario: lease_override deploys with a prominent warning and tells the holder

- **GIVEN** task `A` holds the dogfood lease
- **WHEN** task `B` calls `iris_set_dogfood` with `lease_override = true`
- **THEN** the deploy proceeds, `warnings[0]` begins `LEASE OVERRIDE:` and names `A`, iris messages `A` that `B` replaced the dogfood tree with the new SHA, and `A` still holds the lease

#### Scenario: lease_override does not relax the ancestry refusal

- **GIVEN** task `B` does not hold the lease and passes `lease_override = true` but not `force`
- **WHEN** the supplied SHA is not a descendant of the current dogfood SHA
- **THEN** iris refuses with the commit-dropping error, exactly as for a lease holder

#### Scenario: force does not relax the lease gate

- **GIVEN** task `B` does not hold the lease
- **WHEN** `B` calls `iris_set_dogfood` with `force = true` and no `lease_override`
- **THEN** iris refuses with the lease error

#### Scenario: CLI call without a task_id needs lease_override

- **WHEN** a human runs `iris set-dogfood --sha <sha> --manifest <path>` with no task ID
- **THEN** iris refuses with the lease error; re-running with `--lease-override` deploys with the `LEASE OVERRIDE:` warning

#### Scenario: Holder is not released mid-deploy

- **GIVEN** task `A` holds the lease, `B` is queued, and `A` is in an `iris_set_dogfood` call whose reload is running
- **WHEN** a silence deadline or the idle-release deadline passes during the reload
- **THEN** iris does not release `A` until the call finishes; the call's finish counts as contact
