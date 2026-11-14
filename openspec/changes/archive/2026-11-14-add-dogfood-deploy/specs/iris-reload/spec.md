## MODIFIED Requirements

### Requirement: Pre-flight refusals before any side effect

Before acquiring the lock or performing any mutation (including the pull), `iris:reload` SHALL run the tree-state pre-flight checks that do not depend on `.iris.toml` content and refuse with a structured error if any fails. `.iris.toml` content refusals (missing file, malformed TOML, schema/mechanism validation) are NOT part of this requirement — they run after the pull, against the post-pull configuration (see "Configuration is loaded and validated after the pull").

To choose the fetch target, `iris:reload` MAY perform a lenient pre-pull read of `.iris.toml` to discover a `default_branch` override and configured dogfood branch; this lenient read SHALL NOT produce any refusal — on a missing, malformed, or otherwise unreadable file it yields no override and the default-branch fallback (git `origin/HEAD` → `main`) applies.

#### Scenario: Refuses dirty working tree

- **WHEN** the resolved source repo has uncommitted changes
- **THEN** iris returns a structured error naming the dirty paths and does NOT acquire the lock or pull

#### Scenario: Refuses unrelated branch

- **WHEN** the source repo's HEAD is on a branch other than the resolved default branch or configured dogfood branch
- **THEN** iris returns a structured error naming the current branch and the default, and does NOT acquire the lock or pull

#### Scenario: Lenient pre-pull peek resolves the fetch target without refusing

- **WHEN** the pre-pull on-disk `.iris.toml` is missing, malformed, or contains a field the running binary does not recognise
- **THEN** iris does NOT refuse at this step; it resolves `default_branch` from the file's override if the lenient read succeeds, otherwise falls back to git `origin/HEAD` (then `main` with a warning), and proceeds to the pull

#### Scenario: Reload while dogfood is checked out

- **WHEN** the configured dogfood branch is checked out
- **THEN** reload builds, restarts and verifies that tree under the source lock, skips upstream pulls with a warning, and leaves dogfood checked out

### Requirement: Optional caller-supplied build branch

`iris:reload` SHALL accept an optional caller-supplied build branch (an internal `ReloadInput` field, not a new MCP/CLI input). When empty — the case for every existing caller — reload behaves exactly as before, building and restarting against whatever branch is currently checked out. When non-empty, reload SHALL, after acquiring the per-source-repo lock and before loading `.iris.toml` and running the build, check out the named branch in the source repo, so the configuration load, build, and restart all act on that branch's tree (the composed SHA). The selected build branch SHALL remain checked out after build, restart and verification, including failure paths.

#### Scenario: Dogfood is the resting branch

- **WHEN** set_dogfood supplies the configured dogfood build branch
- **THEN** reload checks it out before loading build configuration and leaves it checked out after restart

### Requirement: Pull behavior

Iris SHALL fetch and fast-forward-only merge origin/<default-branch> before invoking the build step, unless the caller passes `no_pull = true` or the configured dogfood branch is checked out.

#### Scenario: Default behavior pulls

- **WHEN** `iris:reload` is invoked on the default branch without `no_pull`
- **THEN** iris runs `git fetch origin <default-branch>` followed by `git merge --ff-only origin/<default-branch>` and records `pulled = true` in the result

#### Scenario: `no_pull` skips the pull

- **WHEN** `iris:reload` is invoked with `no_pull = true`
- **THEN** iris does NOT fetch or merge, records `pulled = false`, and uses the current HEAD for the build

#### Scenario: Refuses divergent history

- **WHEN** local default branch and origin/<default-branch> have diverged (fast-forward is not possible)
- **THEN** iris returns a structured error naming both SHAs and does NOT build

#### Scenario: Dogfood preserves composite history

- **WHEN** reload is invoked on the configured dogfood branch without no_pull
- **THEN** iris skips fetch/merge with a warning and builds its current HEAD; upstream composition requires iris_dogfood_deploy

### Requirement: Per-source-repo lock spans pull, build, and restart

Iris SHALL hold the per-source-repo lock from immediately after pre-flight through restart and configured readiness verification (for cross-reload) or through response flush (for self-reload, releasing before scheduling `os.Exit`).

#### Scenario: Concurrent reloads on the same source repo serialize

- **WHEN** two `iris:reload` calls target the same source repo concurrently
- **THEN** the second call blocks until the first releases the lock

#### Scenario: Lock released before self-reload exit

- **WHEN** a self-reload reaches the exit step
- **THEN** the lock is released before `os.Exit(75)` is scheduled, so the respawned daemon starts with a fresh lock map

## ADDED Requirements

### Requirement: Dogfood reload lease

Reload from the configured dogfood branch SHALL enter the dogfood lease gate, including flight marking, unless invoked internally under the already gated deploy lock. A non-holder MUST explicitly use lease_override to reload dogfood. Default-branch reload SHALL retain its existing lease-free behavior.

#### Scenario: Dogfood reload by a non-holder

- **WHEN** a non-holder reloads a dogfood checkout without override
- **THEN** reload refuses with the actionable LeaseGateError and performs no build or restart
