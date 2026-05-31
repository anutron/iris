## MODIFIED Requirements

### Requirement: Pre-flight refusals before any side effect

Before acquiring the lock or performing any mutation (including the pull), `iris:reload` SHALL run the tree-state pre-flight checks that do not depend on `.iris.toml` content and refuse with a structured error if any fails. `.iris.toml` content refusals (missing file, malformed TOML, schema/mechanism validation) are NOT part of this requirement — they run after the pull, against the post-pull configuration (see "Configuration is loaded and validated after the pull").

To choose the fetch target, `iris:reload` MAY perform a lenient pre-pull read of `.iris.toml` solely to discover a `default_branch` override; this lenient read SHALL NOT produce any refusal — on a missing, malformed, or otherwise unreadable file it yields no override and the default-branch fallback (git `origin/HEAD` → `main`) applies.

#### Scenario: Refuses dirty working tree

- **WHEN** the resolved source repo has uncommitted changes
- **THEN** iris returns a structured error naming the dirty paths and does NOT acquire the lock or pull

#### Scenario: Refuses non-default branch

- **WHEN** the source repo's HEAD is on a branch other than the resolved default branch
- **THEN** iris returns a structured error naming the current branch and the default, and does NOT acquire the lock or pull

#### Scenario: Lenient pre-pull peek resolves the fetch target without refusing

- **WHEN** the pre-pull on-disk `.iris.toml` is missing, malformed, or contains a field the running binary does not recognise
- **THEN** iris does NOT refuse at this step; it resolves `default_branch` from the file's override if the lenient read succeeds, otherwise falls back to git `origin/HEAD` (then `main` with a warning), and proceeds to the pull

### Requirement: Optional pre-flight and verify hooks

Iris SHALL support optional `[pre_flight]` and `[verify]` blocks. `[pre_flight]` runs after the pull and after iris's post-pull configuration validation, but before the build; non-zero exit aborts (no build, no restart). `[verify]` runs after restart for cross-reload only; non-zero exit returns an error but does NOT roll back the restart.

#### Scenario: Pre-flight hook aborts on non-zero exit

- **WHEN** `[pre_flight] command` returns exit code 1
- **THEN** iris returns a structured error containing the captured output, does NOT build or restart, and releases the lock

#### Scenario: Pre-flight hook runs against the freshly-pulled tree

- **WHEN** a reload pulls new commits and `.iris.toml` declares a `[pre_flight]` block
- **THEN** iris runs the post-pull `[pre_flight]` command (reading the hook definition from the post-pull `.iris.toml`) after the fast-forward and before the build

#### Scenario: Verify hook reports failure without rollback

- **WHEN** the restart succeeds but `[verify] command` returns exit code 1
- **THEN** iris returns a structured error containing the verify output; the daemon remains on the new binary

#### Scenario: Verify is skipped for self-reload

- **WHEN** the reload is self-managed
- **THEN** any `[verify]` block in `.iris.toml` is ignored (iris has exited; nothing in-process can verify)

## ADDED Requirements

### Requirement: Configuration is loaded and validated after the pull

`iris:reload` SHALL load and validate `.iris.toml` AFTER the fetch + fast-forward-merge (or, with `no_pull = true`, after the no-op pull step), against the post-pull on-disk file — the configuration the rebuilt-and-restarted binary will actually consume. The structured failure shape (`valid: false` with an `errors` array of `{ field, message, hint }`) is preserved; it simply surfaces at this later step. Every `.iris.toml` content refusal — missing file, malformed TOML, unsupported `schema_version`, cross-mechanism field mismatch, `exit_code` legality — SHALL fire here, not before the pull.

#### Scenario: Validation runs against the post-pull config

- **WHEN** a reload pulls new commits that change `.iris.toml`
- **THEN** iris validates the post-pull `.iris.toml` (not the pre-pull file) and uses the post-pull `[build]` and `[restart]` blocks for the build and restart

#### Scenario: Refuses missing `.iris.toml` after the pull

- **WHEN** the source repo root does not contain a readable `.iris.toml` after the pull
- **THEN** iris returns a structured error naming the expected path, and does so after the pull rather than before it

#### Scenario: Refuses malformed TOML after the pull

- **WHEN** the post-pull `.iris.toml` is not syntactically valid TOML
- **THEN** iris returns a structured error naming the parse failure (with a line number when available) and does NOT build or restart

#### Scenario: Refuses unknown schema_version after the pull

- **WHEN** the post-pull `.iris.toml` is missing `schema_version` or sets it to a value iris does not support
- **THEN** iris returns a structured error naming the field, the offending value, and the supported versions — even when unknown-field tolerance is in effect

#### Scenario: Refuses cross-mechanism field mismatch after the pull

- **WHEN** the post-pull `.iris.toml` declares `[restart] mechanism = "launchagent"` but also sets a `pid_file` field (which only belongs to the `signal` mechanism)
- **THEN** iris returns a structured error naming both the declared mechanism and the conflicting field

#### Scenario: Refuses exit_code mechanism for cross-reload after the pull

- **WHEN** the post-pull `.iris.toml` declares `[restart] mechanism = "exit_code"` but the resolved source repo is NOT iris's own deployed source repo
- **THEN** iris returns a structured error stating that `exit_code` is a self-only mechanism

### Requirement: Forward-compatible unknown fields are tolerated during reload pre-flight

During reload pre-flight, `iris:reload` SHALL decode `.iris.toml` in a forward-compatible mode in which unknown fields (top-level or nested) are downgraded from validation errors to non-fatal warnings. Each tolerated unknown field SHALL be surfaced as a warning in the structured result's `warnings` and in the audit-log entry. This makes an additive `.iris.toml` schema change deployable in a single reload: the freshly-pulled new field is tolerated by the old binary's decoder, the build produces the new binary, and the restart brings up a binary that fully understands the field. This tolerance applies ONLY to reload (and publish) pre-flight; `iris:validate_config` SHALL remain strict.

#### Scenario: Unknown field is tolerated and the reload proceeds

- **WHEN** the post-pull `.iris.toml` contains a field the running binary does not recognise, but is otherwise valid
- **THEN** iris does NOT refuse; it records a warning naming the unknown field, proceeds to build and restart, and includes the warning in the result and audit entry

#### Scenario: schema_version mismatch is not tolerated

- **WHEN** the post-pull `.iris.toml` sets `schema_version` to an unsupported value (in addition to any unknown fields)
- **THEN** iris still refuses with a structured `schema_version` error — version mismatch is a hard refusal even though unknown fields are tolerated

#### Scenario: Malformed TOML is not tolerated

- **WHEN** the post-pull `.iris.toml` fails to parse as TOML
- **THEN** iris still refuses with a structured parse error — forward-compat tolerance applies to unknown fields, not to syntax errors
