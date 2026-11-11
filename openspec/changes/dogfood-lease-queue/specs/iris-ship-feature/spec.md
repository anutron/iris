## ADDED Requirements

### Requirement: Post-ship recompose respects the dogfood lease

When a `via: "pr-auto"` ship would re-compose the dogfood branch, iris SHALL check the source repo's dogfood lease before calling `set_dogfood`:

- **Lease free, or held by the shipping task:** iris SHALL run the recompose as an internal caller that passes the lease gate without a `LEASE OVERRIDE:` warning. When the shipping task holds the lease, the recompose SHALL count as its lease contact.
- **Lease held by another task:** iris SHALL skip the recompose and leave the dogfood branch and manifest unchanged. The result SHALL report `recompose: { attempted: false, succeeded: false }` and a warning with `code: "recompose_skipped"` whose message says dogfood is leased to the holder (task ID and name). Iris SHALL message the holder that the shipped branch landed on the default branch and that it may want to recompose dogfood.

The ship itself (PR, merge, fetch) SHALL succeed regardless of the lease; only the recompose is affected.

#### Scenario: Recompose skipped while another task holds the lease

- **GIVEN** a dogfood manifest layering features `F1` and `F2`, and task `B` holding the dogfood lease
- **WHEN** task `A` ships `F1` with `via: "pr-auto"` and the merge succeeds
- **THEN** iris reports `shipped: true`, `recompose: { attempted: false, succeeded: false }` and a `recompose_skipped` warning whose message says dogfood is leased to `B`
- **AND** the dogfood branch and manifest are unchanged, and iris messages `B` that `F1` shipped

#### Scenario: Recompose runs when the lease is free

- **GIVEN** a dogfood manifest layering `F1` and `F2` and no lease holder
- **WHEN** task `A` ships `F1` with `via: "pr-auto"` and the merge succeeds
- **THEN** iris re-composes dogfood with `F1` removed, and the result carries no `LEASE OVERRIDE:` warning

#### Scenario: Recompose by the holder counts as contact

- **GIVEN** task `A` holds the dogfood lease
- **WHEN** `A` ships a layered feature with `via: "pr-auto"`
- **THEN** iris runs the recompose and restarts `A`'s warmer clock
