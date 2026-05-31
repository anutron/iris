## MODIFIED Requirements

### Requirement: Pre-flight refusals before any side effect

`iris:publish` SHALL perform pre-flight checks before acquiring the lock or mutating any state, and SHALL refuse with a structured error if any fails. Because `iris:publish` advances the source repo to the **worktree's** HEAD, the configuration that will run after the update is the worktree's `.iris.toml`; the `.iris.toml` pre-flight check therefore SHALL load and validate the worktree's `.iris.toml` (the configuration being published), not the source repo's pre-publish `.iris.toml`. The structured `valid: false` / `errors` shape is preserved.

#### Scenario: Refuses dirty worktree

- **WHEN** the argus worktree has uncommitted changes (`git status --porcelain` returns non-empty)
- **THEN** iris returns a structured error naming the dirty paths and does NOT acquire the lock or touch the source repo

#### Scenario: Refuses dirty source repo

- **WHEN** the source repo's working tree has uncommitted changes
- **THEN** iris returns a structured error naming the dirty paths and does NOT acquire the lock (avoids clobbering operator's in-progress work via `--reset` or surprising the operator with a half-applied state via `--ff`)

#### Scenario: Refuses missing or invalid published `.iris.toml`

- **WHEN** the worktree root does not contain a readable `.iris.toml`, or the worktree's `.iris.toml` fails schema validation
- **THEN** iris returns a structured error naming the expected path and validation errors and does NOT acquire the lock or mutate the source repo

#### Scenario: Refuses source repo outside argus project allowlist

- **WHEN** the resolved source-repo path does not match any allowlisted argus project
- **THEN** iris returns a structured error naming the rejected path and does NOT acquire the lock

#### Scenario: Refuses when target branch is not source repo's current HEAD

- **WHEN** the resolved `branch` (either supplied or defaulted) is NOT the source repo's currently-checked-out branch
- **THEN** iris returns a structured error naming both the requested branch and the source repo's actual current branch, and does NOT mutate the source repo (v1.2 constraint – a future flag may add checkout behavior)

#### Scenario: Refuses non-ancestor worktree HEAD without --reset

- **WHEN** the worktree's HEAD is NOT an ancestor-descendant of the source repo's branch tip and `reset = false`
- **THEN** iris returns a structured error naming both SHAs and pointing at `--reset`, and does NOT advance the ref

## ADDED Requirements

### Requirement: Validation targets the published configuration

`iris:publish` SHALL validate the `.iris.toml` it is about to publish — the worktree's `.iris.toml` at the worktree HEAD — rather than the source repo's stale pre-publish `.iris.toml`. Because the clean-worktree pre-flight check guarantees the worktree's working tree equals its HEAD, and the publish sets the source repo to that same HEAD, the worktree's `.iris.toml` is exactly the configuration the rebuilt binary will consume after the update. This validates the post-update truth without reordering past the (potentially destructive `--reset`) mutation.

#### Scenario: Worktree config is the validated config

- **WHEN** the worktree's `.iris.toml` differs from the source repo's current `.iris.toml` (the worktree is ahead)
- **THEN** iris validates the worktree's `.iris.toml`, and the publish succeeds or fails based on the worktree's config — the one that will run after the update

#### Scenario: Invalid worktree config is refused before the mutation

- **WHEN** the worktree's `.iris.toml` fails schema validation
- **THEN** iris refuses before acquiring the lock and before any `merge --ff-only` or `reset --hard`, leaving the source repo untouched

### Requirement: Forward-compatible unknown fields are tolerated during publish pre-flight

During publish pre-flight, `iris:publish` SHALL decode the worktree's `.iris.toml` in a forward-compatible mode in which unknown fields (top-level or nested) are downgraded from validation errors to non-fatal warnings, surfaced in the structured result's `warnings` and in the audit-log entry. This makes publishing a worktree that introduces an additive `.iris.toml` field succeed in a single call: the old running daemon tolerates the new field, then the rebuilt binary understands it. The tolerance applies to unknown fields only; `schema_version` mismatch and malformed TOML remain hard refusals, and `iris:validate_config` remains strict.

#### Scenario: Unknown field in the worktree config is tolerated

- **WHEN** the worktree's `.iris.toml` contains a field the running binary does not recognise, but is otherwise valid
- **THEN** iris does NOT refuse; it records a warning naming the unknown field, performs the update, build, and restart, and includes the warning in the result and audit entry

#### Scenario: schema_version mismatch in the worktree config is not tolerated

- **WHEN** the worktree's `.iris.toml` sets `schema_version` to an unsupported value
- **THEN** iris still refuses with a structured `schema_version` error before mutating the source repo
