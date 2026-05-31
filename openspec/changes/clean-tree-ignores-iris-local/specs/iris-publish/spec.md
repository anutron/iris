# iris-publish Specification

## ADDED Requirements

### Requirement: Clean-tree pre-flight ignores `.iris.local.toml`

`iris:publish`'s pre-flight working-tree cleanliness checks — on both the argus worktree and the source repo — SHALL NOT treat the presence of `.iris.local.toml` as a dirty working tree. `.iris.local.toml` is iris's own managed per-developer overlay; publish MUST tolerate it. Any other uncommitted change SHALL still make the tree dirty and SHALL still cause publish to refuse before acquiring the lock or touching the source repo.

#### Scenario: Untracked `.iris.local.toml` alone does not block publish

- **GIVEN** a source repo (or argus worktree) whose only uncommitted change is an untracked `.iris.local.toml`
- **WHEN** `iris:publish` runs its pre-flight clean-tree checks
- **THEN** the checks pass — publish does NOT refuse as "dirty" on account of `.iris.local.toml`

#### Scenario: A real uncommitted change still refuses

- **GIVEN** a source repo (or argus worktree) with another uncommitted change alongside `.iris.local.toml`
- **WHEN** `iris:publish` runs its pre-flight clean-tree checks
- **THEN** publish still refuses as "dirty", naming the real change but not `.iris.local.toml`
