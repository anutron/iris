# iris-set-dogfood Specification

## ADDED Requirements

### Requirement: Bootstrap resolution of `dogfood_branch` without `.iris.toml`

`iris:set_dogfood` SHALL resolve `dogfood_branch` even when the source repo root has no `.iris.toml`. When the merged overlay yields no `dogfood_branch` because `.iris.toml` is absent (the bootstrap case — the shared config lives on the dogfood branch, not yet on the default branch), the verb SHALL read `dogfood_branch` directly from `.iris.local.toml` at the source repo root. `dogfood_branch` is a `local`-tagged field and is the pointer to the branch that carries `.iris.toml`, so its resolution MUST NOT be gated behind `.iris.toml` existing. When `dogfood_branch` is set in neither the merged overlay nor `.iris.local.toml`, the verb SHALL refuse as before.

When `.iris.toml` IS present at the source root, `dogfood_branch` resolution is unchanged (it comes from the merged overlay).

#### Scenario: Resolves dogfood_branch from .iris.local.toml when the default branch has no .iris.toml

- **GIVEN** a source repo on its default branch with NO `.iris.toml` at the root, a `.iris.local.toml` declaring `dogfood_branch = "dev"`, and a composed SHA whose tree DOES carry a valid `.iris.toml`
- **WHEN** `iris:set_dogfood` is invoked with that SHA
- **THEN** iris resolves `dogfood_branch = "dev"` from `.iris.local.toml`, points `dev` at the composed SHA, and runs the reload against the `dev` tree — it does NOT refuse as "dogfood_branch not configured"

#### Scenario: Still refuses when dogfood_branch is set nowhere

- **GIVEN** a source repo with no `.iris.toml` at the root AND no `dogfood_branch` in `.iris.local.toml` (or no `.iris.local.toml` at all)
- **WHEN** `iris:set_dogfood` is invoked
- **THEN** iris refuses with the `dogfood_branch not configured` error and performs no git mutation, manifest write, or reload
