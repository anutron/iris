# iris-reload Specification

## ADDED Requirements

### Requirement: Clean-tree pre-flight ignores `.iris.local.toml`

`iris:reload`'s pre-flight working-tree cleanliness check SHALL NOT treat the presence of `.iris.local.toml` at the source repo root as a dirty working tree. `.iris.local.toml` is iris's own managed per-developer overlay — written by `iris:set_local_config` and expected to live at the source-repo root, gitignored once the project's default branch adopts iris — so reload MUST tolerate it whether it is untracked, ignored, or modified. Any other uncommitted change SHALL still make the tree dirty and SHALL still cause the pre-flight to refuse.

This keeps the dogfood bootstrap coherent: `set_local_config` writes `.iris.local.toml`, and the subsequent `set_dogfood` → `reload` does not refuse because of the file iris itself just wrote, even before the default branch carries the gitignore entry.

#### Scenario: Untracked `.iris.local.toml` alone is not dirty

- **GIVEN** a source repo whose only uncommitted change is an untracked `.iris.local.toml` at the root (the default branch does not yet gitignore it)
- **WHEN** `iris:reload` runs its pre-flight clean-tree check
- **THEN** the check passes — reload does NOT refuse as "working tree is dirty" and proceeds

#### Scenario: A real uncommitted change still refuses

- **GIVEN** a source repo with an untracked `.iris.local.toml` AND another uncommitted change (a modified or untracked tracked-path file)
- **WHEN** `iris:reload` runs its pre-flight clean-tree check
- **THEN** the check still fails — reload refuses as "working tree is dirty", and the reported dirty paths name the real change but NOT `.iris.local.toml`
