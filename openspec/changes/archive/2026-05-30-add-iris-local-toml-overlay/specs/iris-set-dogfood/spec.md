## ADDED Requirements

### Requirement: Manifest captures 1-deep `previous_manifest`

When `iris:set_dogfood` overwrites an existing dogfood manifest, the new manifest SHALL embed the prior manifest's full contents under a `previous_manifest` field. The embedded prior manifest's own `previous_manifest` field SHALL be stripped before embedding (no recursion beyond one level). When no prior manifest exists, `previous_manifest` SHALL be omitted entirely (NOT set to `null`).

#### Scenario: previous_manifest captures the prior state

- **GIVEN** a source repo with an existing `dogfood-manifest.json` containing `{ base, layered: [F1, F2], recorded_at }`
- **WHEN** `iris:set_dogfood` succeeds with a new manifest `{ base', layered: [F2, F3] }`
- **THEN** the persisted manifest is `{ base', layered: [F2, F3], recorded_at: <now>, previous_manifest: { base, layered: [F1, F2], recorded_at: <prior> } }`
- **AND** the embedded `previous_manifest` does NOT itself contain a `previous_manifest` field

#### Scenario: previous_manifest is omitted on first dogfood

- **GIVEN** a source repo with no existing `dogfood-manifest.json`
- **WHEN** `iris:set_dogfood` succeeds for the first time
- **THEN** the persisted manifest has NO `previous_manifest` key (not set, not `null` — absent)

#### Scenario: previous_manifest depth is bounded at 1

- **GIVEN** three successive `iris:set_dogfood` calls with manifests A, B, C
- **WHEN** all three succeed
- **THEN** after the third call, the on-disk manifest is `{ ...C, previous_manifest: { ...B } }` and the embedded B does NOT contain A

#### Scenario: previous_manifest survives manifest read

- **GIVEN** a manifest on disk with `previous_manifest`
- **WHEN** `iris:status` is invoked
- **THEN** the `dogfood` field in the result contains the full manifest including `previous_manifest`
