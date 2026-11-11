# iris-set-dogfood Specification

## Purpose

Points a managed system's dogfood branch at a worker-supplied commit, records a manifest of what that commit contains, and reloads the system.
## Requirements
### Requirement: `iris:set_dogfood` verb

The plugin SHALL expose `iris:set_dogfood` as an MCP tool and CLI subcommand that, for one managed system, atomically hard-resets the configured dogfood branch to a worker-supplied commit SHA, persists a structured manifest describing what that SHA contains, and triggers the existing reload/build/restart machinery against the composed SHA. The verb SHALL resolve `dogfood_branch` from the MERGED configuration — `.iris.toml` overlaid with the optional `.iris.local.toml` (`dogfood_branch` is a `local`-tagged field). The verb SHALL refuse to operate on any repo whose merged configuration does not declare `dogfood_branch`. Overlay taxonomy warnings (for example, a `local`-tagged field left in `.iris.toml`) SHALL be propagated into the result's `warnings`.

The reload the verb triggers SHALL build and restart against the composed SHA — that is, the dogfood branch's tree, not the default branch's tree (see `iris-reload`'s caller-supplied build-branch requirement). Because the build runs against the composed SHA, the `[build]` and `[restart]` configuration consumed for the reload comes from that SHA's `.iris.toml`.

Inputs:

- `task_id` (string, optional) — same resolution semantics as other verbs; defaults to iris-on-iris when omitted.
- `sha` (string, required) — a full git commit SHA reachable from the source repo's object database.
- `manifest` (object, required) — structured record of what composes the SHA:
  - `base` (object): `{ ref: string, sha: string }` — the upstream base (e.g., `main`) and its SHA at compose time.
  - `layered` (array, optional): ordered list of `{ name: string, sha: string, applied?: string }` describing each branch the agent composed in. `applied` is descriptive (e.g., `"cherry-pick"`, `"merge"`) and is not validated by Iris.
  - `note` (string, optional) — free-text from the agent.

Result shape:

- `set` (bool) — `true` on success.
- `dogfood_branch` (string) — the branch name from the merged config.
- `previous_sha` (string) — the dogfood branch's SHA before the reset.
- `new_sha` (string) — the SHA passed in.
- `reload` (object) — the same reload result the existing reload verb returns.
- `warnings` (array) — structured non-fatal warnings, including any overlay taxonomy warnings and any from the reload.

#### Scenario: Sets dogfood branch and triggers reload on a valid request

- **GIVEN** a source repo whose merged config declares `dogfood_branch = "dev"` and a valid `[build]`/`[restart]` block
- **AND** the worker has produced a commit SHA `abc123` reachable in the source repo
- **WHEN** `iris:set_dogfood` is invoked with `sha = "abc123"` and a well-formed manifest
- **THEN** iris persists the manifest to the iris state directory, hard-resets the `dev` branch to `abc123`, and runs the same reload sequence used by `iris:reload` against the `dev` branch's tree
- **AND** returns `{ set: true, dogfood_branch: "dev", previous_sha: "<prior>", new_sha: "abc123", reload: { ... } }`

#### Scenario: Resolves dogfood_branch from .iris.local.toml

- **GIVEN** a source repo whose `.iris.toml` does NOT declare `dogfood_branch` but whose gitignored `.iris.local.toml` declares `dogfood_branch = "dev"`
- **WHEN** `iris:set_dogfood` is invoked with a reachable SHA and a well-formed manifest
- **THEN** iris resolves `dogfood_branch = "dev"` from the overlay, sets the branch, and runs the reload — it does NOT refuse as "not configured"

#### Scenario: Refuses when dogfood_branch is unset in both files

- **GIVEN** a source repo whose merged config (`.iris.toml` plus any `.iris.local.toml`) does not declare `dogfood_branch`
- **WHEN** `iris:set_dogfood` is invoked
- **THEN** iris returns an error of the form `dogfood_branch not configured for this repo (add dogfood_branch = "..." to .iris.local.toml)` and performs no git mutations, no manifest writes, and no reload

#### Scenario: Refuses when SHA is not reachable

- **GIVEN** a source repo with `dogfood_branch = "dev"` configured
- **WHEN** `iris:set_dogfood` is invoked with a `sha` that `git rev-parse --verify <sha>^{commit}` cannot resolve
- **THEN** iris returns an error naming the unreachable SHA and performs no mutations

#### Scenario: Build deploys the composed SHA, not the default branch

- **GIVEN** a source repo on its default branch whose `dev` dogfood branch is reset to a composed SHA whose tree differs from the default branch's tree
- **WHEN** `iris:set_dogfood` runs the reload
- **THEN** the `[build] command` executes against the `dev` branch's tree (the composed SHA), not the default branch's tree
- **AND** after the reload completes, the source repo is checked back out on its default branch

#### Scenario: Manifest is persisted alongside the audit log

- **WHEN** `iris:set_dogfood` succeeds
- **THEN** iris writes the manifest as `dogfood-manifest.json` in the same per-source-repo state directory as the audit log, overwriting any prior manifest

#### Scenario: Manifest write precedes branch reset

- **GIVEN** a successful `iris:set_dogfood` invocation
- **WHEN** the manifest write fails (disk full, permissions, etc.)
- **THEN** iris returns the error and does NOT reset the dogfood branch — the branch remains at `previous_sha`

#### Scenario: Branch reset failure leaves manifest ahead

- **GIVEN** a successful manifest write
- **WHEN** the subsequent git reset fails
- **THEN** iris returns the error; the manifest file reflects the intended state while the branch SHA reflects the prior state, and `iris:status` will report drift between them on next call

#### Scenario: Dogfood branch is created if missing

- **GIVEN** a source repo with `dogfood_branch = "dev"` and no local `dev` branch
- **WHEN** `iris:set_dogfood` is invoked with a valid SHA
- **THEN** iris creates the `dev` branch at the supplied SHA, persists the manifest, runs reload, and returns `previous_sha: ""` to indicate the branch was newly created

#### Scenario: Concurrent set_dogfood calls serialize via the source-repo lock

- **GIVEN** two simultaneous `iris:set_dogfood` invocations targeting the same source repo
- **WHEN** both are dispatched
- **THEN** they serialize through the existing per-source-repo mutex; the second runs to completion after the first, with its own manifest overwriting the first

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris set-dogfood --sha <sha> --manifest <json-path>` from any shell
- **THEN** the same `verbs.SetDogfood` Go function executes and prints the structured result as pretty-printed JSON

### Requirement: Dogfood manifest schema

The persisted manifest file SHALL be JSON conforming to the input `manifest` shape, with `base.ref`, `base.sha`, and at least an empty `layered: []` array. The file SHALL include a `recorded_at` ISO-8601 timestamp added by iris at write time.

#### Scenario: Manifest includes recorded_at when persisted

- **WHEN** iris writes the manifest
- **THEN** the file includes `recorded_at` populated with the current UTC time in ISO-8601 format, regardless of whether the input manifest contained one

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

### Requirement: Dogfood lease gate

`iris:set_dogfood` SHALL refuse a caller that does not hold the source repo's dogfood lease (see `iris-dogfood-lease`), unless the new `lease_override` input is set. The caller is the `task_id` input. A call with no `task_id` (CLI or iris-on-iris default) has no lease identity and SHALL be treated as not holding the lease. The gate SHALL run after `dogfood_branch` resolution and SHA reachability, and before the ancestry check, the manifest write, any git mutation and any reload, so a refusal has no side effects.

The refusal error SHALL:

- name the current holder by task ID and argus task name, or say that no one holds the lease;
- list every queued task ahead of the caller, by task ID and argus task name in queue order (the whole queue when the caller is not queued);
- give the caller's own queue position, if it is queued;
- point the caller to `iris_dogfood_claim`, or to `lease_override` to bypass the lease.

The same information SHALL also be available as structured fields (`holder`, `ahead`, `position`) on the error response, so an agent does not have to parse the text.

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
- **THEN** iris returns an error naming `A` (task ID and argus task name) as holder, `B`'s position 1, and `iris_dogfood_claim` / `lease_override`
- **AND** iris writes no manifest, moves no ref, and runs no reload

#### Scenario: Refusal lists everyone ahead of an unqueued caller

- **GIVEN** task `A` holds the dogfood lease and `B`, then `C`, are queued
- **WHEN** task `D`, which is not queued, calls `iris_set_dogfood` without `lease_override`
- **THEN** the error names `A` by argus task name as holder and lists `B` then `C` (with their argus task names) as ahead, and the structured `ahead` field carries the same two entries in order

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

