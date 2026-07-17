# iris-set-dogfood Specification

## MODIFIED Requirements

### Requirement: `iris:set_dogfood` verb

The plugin SHALL expose `iris:set_dogfood` as an MCP tool and CLI subcommand that, for one managed system, atomically hard-resets the configured dogfood branch to a worker-supplied commit SHA, persists a structured manifest describing what that SHA contains, and triggers the existing reload/build/restart machinery against the composed SHA. The verb SHALL resolve `dogfood_branch` from the MERGED configuration — `.iris.toml` overlaid with the optional `.iris.local.toml` (`dogfood_branch` is a `local`-tagged field). The verb SHALL refuse to operate on any repo whose merged configuration does not declare `dogfood_branch`. Overlay taxonomy warnings (for example, a `local`-tagged field left in `.iris.toml`) SHALL be propagated into the result's `warnings`.

The reload the verb triggers SHALL build and restart against the composed SHA — that is, the dogfood branch's tree, not the default branch's tree (see `iris-reload`'s caller-supplied build-branch requirement). Because the build runs against the composed SHA, the `[build]` and `[restart]` configuration consumed for the reload comes from that SHA's `.iris.toml`.

The verb SHALL move the dogfood ref with a worktree-guarded strategy: when the dogfood branch is NOT checked out in any worktree of the source repo it SHALL use `git branch -f` (the ref-only contract); when the dogfood branch IS checked out in a worktree it SHALL use `git reset --hard <sha>` in that worktree so the ref, HEAD, index, and working tree move together. The verb SHALL NOT use `git update-ref` for a checked-out branch (it would leave HEAD/index disagreeing with the tree). The create-when-absent path (the dogfood branch does not yet exist) is unaffected.

The verb SHALL guard against dropping commits: when the dogfood branch already exists and the supplied `sha` is not a descendant of the branch's current SHA, the verb SHALL refuse (naming how many commits would be dropped and the current `previous_sha`) unless `force` is set. This check is described in the "Commit-dropping deploys are refused unless forced" requirement.

Inputs:

- `task_id` (string, optional) — same resolution semantics as other verbs; defaults to iris-on-iris when omitted.
- `sha` (string, required) — a full git commit SHA reachable from the source repo's object database.
- `force` (bool, optional, default `false`) — override the commit-dropping ancestry refusal. When `true`, a non-descendant `sha` is deployed anyway and a prominent warning is emitted. It relaxes ONLY the ancestry refusal; it does not affect config resolution, SHA reachability, the worktree guard, or reload.
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
- `warnings` (array) — structured non-fatal warnings, including any overlay taxonomy warnings, any from the reload, and the commit-dropping warning emitted when `force` overrides the ancestry refusal.

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

#### Scenario: Ref move resets a checked-out dogfood branch

- **GIVEN** a source repo whose `dev` dogfood branch already exists and is checked out in a worktree
- **AND** a composed `sha` that is a descendant of `dev`'s current SHA
- **WHEN** `iris:set_dogfood` is invoked
- **THEN** iris moves `dev` to the composed `sha` via `git reset --hard` in the worktree that has `dev` checked out (not `git branch -f`, which git refuses for a checked-out branch)
- **AND** `dev` points at the composed `sha` afterward

#### Scenario: Manifest is persisted alongside the audit log

- **WHEN** `iris:set_dogfood` succeeds
- **THEN** iris writes the manifest as `dogfood-manifest.json` in the same per-source-repo state directory as the audit log, overwriting any prior manifest

#### Scenario: Manifest write precedes branch reset

- **GIVEN** a successful `iris:set_dogfood` invocation
- **WHEN** the manifest write fails (disk full, permissions, etc.)
- **THEN** iris returns the error and does NOT reset the dogfood branch — the branch remains at `previous_sha`

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

## ADDED Requirements

### Requirement: Commit-dropping deploys are refused unless forced

`iris:set_dogfood` SHALL NOT silently drop commits from the dogfood branch. When the dogfood branch already exists (`previous_sha` is non-empty) and the supplied `sha` is NOT a descendant of `previous_sha` (`git merge-base --is-ancestor <previous_sha> <sha>` exits non-zero), deploying `sha` would drop every commit reachable from `previous_sha` but not from `sha`. In that case the verb SHALL refuse with an error that names the number of commits that would be dropped (`git rev-list --count <sha>..<previous_sha>`) and the current `previous_sha`, and SHALL perform no manifest write, no git mutation, and no reload — UNLESS `force` is `true`.

When `force` is `true`, the verb SHALL proceed with the deploy and SHALL append a prominent entry to the result's `warnings` naming the number of commits dropped and the `previous_sha`. The check SHALL be skipped when `previous_sha` is empty (the branch is being created — there is nothing to drop). Iris SHALL NOT auto-compose, merge, or cherry-pick to avoid the drop; recomposition is the agent's responsibility.

#### Scenario: Refuses a commit-dropping deploy without force

- **GIVEN** a source repo with `dogfood_branch = "dev"` whose `dev` branch has commits the supplied `sha` does not contain (`sha` is not a descendant of `dev`)
- **WHEN** `iris:set_dogfood` is invoked without `force`
- **THEN** iris returns an error naming how many commits would be dropped and the current `previous_sha`
- **AND** performs no manifest write, no git mutation, and no reload — `dev` still points at `previous_sha`

#### Scenario: Forced commit-dropping deploy proceeds with a warning

- **GIVEN** the same non-descendant `sha` and `dev` branch
- **WHEN** `iris:set_dogfood` is invoked with `force = true`
- **THEN** iris deploys `sha`, moves `dev` to it, and returns `set: true`
- **AND** the result's `warnings` contains a prominent entry naming the number of commits dropped and the `previous_sha`

#### Scenario: Descendant SHA proceeds without a drop warning

- **GIVEN** a source repo with `dogfood_branch = "dev"` and a composed `sha` that IS a descendant of `dev`'s current SHA (or equal to it)
- **WHEN** `iris:set_dogfood` is invoked without `force`
- **THEN** iris deploys normally with no ancestry refusal and no commit-dropping warning
