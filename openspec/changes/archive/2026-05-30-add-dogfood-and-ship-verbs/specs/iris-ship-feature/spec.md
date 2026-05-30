## ADDED Requirements

### Requirement: `iris:ship_feature` verb

The plugin SHALL expose `iris:ship_feature` as an MCP tool and CLI subcommand that ships a feature branch to `origin`'s default branch via a GitHub pull request. The verb SHALL operate in one of two modes selected by the `via` parameter and SHALL refuse any non-supported mode.

Inputs:

- `task_id` (string, optional) — standard resolution.
- `branch` (string, required) — the feature branch to ship. SHALL be a local branch other than the source repo's default branch.
- `via` (string, required) — one of `"pr"` or `"pr-auto"`.
- `pr_title` (string, optional) — title for the PR; defaults to the branch's last commit subject if omitted.
- `pr_body` (string, optional) — body for the PR.
- `merge_method` (string, optional) — `"squash"`, `"merge"`, or `"rebase"`; defaults to `"squash"`. Only used in `pr-auto` mode.

Result shape:

- `shipped` (bool) — `true` when the motion completed its full expected sequence for the chosen `via`.
- `branch` (string) — the branch that was shipped.
- `pr_number` (int) — the PR number created.
- `pr_url` (string) — the PR's URL.
- `merged` (bool) — whether the PR was merged (always `false` for `via: "pr"`, expected `true` on successful `via: "pr-auto"`).
- `merge_sha` (string, optional) — the merge commit SHA on origin's default branch when `merged` is true.
- `fetched` (bool) — whether iris fetched after merging (only relevant for `pr-auto`).
- `recompose` (object, optional) — when a dogfood manifest existed and `via: "pr-auto"` succeeded, describes the post-ship re-compose outcome: `{ attempted: bool, succeeded: bool, new_sha?: string, conflict?: { branch: string, message: string } }`.
- `warnings` (array) — structured non-fatal warnings.

#### Scenario: pr mode pushes branch and opens PR

- **GIVEN** a source repo with feature branch `feature/F2` that has commits ahead of `origin/main`
- **WHEN** `iris:ship_feature` is invoked with `branch = "feature/F2"`, `via = "pr"`
- **THEN** iris pushes `feature/F2` to `origin`, opens a GitHub PR targeting the default branch, and returns `{ shipped: true, branch: "feature/F2", pr_number: <n>, pr_url: "...", merged: false, fetched: false }`

#### Scenario: pr mode does not merge or fetch

- **GIVEN** a successful `via: "pr"` invocation
- **THEN** iris does NOT call the GitHub merge API, does NOT run `git fetch`, and does NOT touch the dogfood branch or manifest

#### Scenario: pr-auto mode pushes, opens, waits for checks, approves, merges, fetches

- **GIVEN** a source repo with feature branch `feature/F2` and `via = "pr-auto"`
- **WHEN** `iris:ship_feature` is invoked
- **THEN** iris executes the following sequence: push branch -> open PR -> wait for required CI checks to pass -> approve PR -> merge PR (using `merge_method`) -> fetch -> attempt dogfood re-compose
- **AND** returns `{ shipped: true, merged: true, merge_sha: "...", fetched: true, recompose: { ... } }`

#### Scenario: pr-auto refuses when CI checks fail

- **GIVEN** a `pr-auto` ship where the opened PR's required checks fail
- **WHEN** iris detects the failure
- **THEN** iris does NOT approve or merge the PR, leaves it open, and returns `{ shipped: false, pr_number: <n>, pr_url: "...", merged: false, warnings: [{ code: "ci_failed", message: "..." }] }`

#### Scenario: pr-auto times out waiting for checks

- **GIVEN** a `pr-auto` ship where CI checks neither pass nor fail within the configured timeout
- **WHEN** the timeout elapses
- **THEN** iris does NOT merge the PR, leaves it open, and returns `{ shipped: false, warnings: [{ code: "ci_timeout", ... }] }`

#### Scenario: pr-auto skips check-waiting when no checks are configured

- **GIVEN** a PR whose head commit has zero required status checks
- **WHEN** iris evaluates `pr-auto`'s check-wait step
- **THEN** iris proceeds immediately to approve and merge without waiting

#### Scenario: Refuses to ship the default branch

- **WHEN** `iris:ship_feature` is invoked with `branch` equal to the source repo's default branch (`main`/`master`)
- **THEN** iris returns an error `refusing to ship default branch "<branch>"` and performs no mutations

#### Scenario: Refuses unknown via mode

- **WHEN** `iris:ship_feature` is invoked with `via` set to anything other than `"pr"` or `"pr-auto"`
- **THEN** iris returns an error naming the supported modes and performs no mutations

#### Scenario: Refuses when branch is missing

- **WHEN** `iris:ship_feature` is invoked with a `branch` that does not exist locally
- **THEN** iris returns an error and performs no mutations

#### Scenario: Recompose drops the shipped feature from the manifest

- **GIVEN** a successful `pr-auto` ship of `feature/F2`
- **AND** the current dogfood manifest's `layered` array contains an entry whose `name` matches `feature/F2`
- **WHEN** iris attempts the post-ship re-compose
- **THEN** iris removes that entry from the manifest, fetches new `main`, and re-applies the remaining `layered` entries against the new base

#### Scenario: Recompose preserves dogfood state on conflict

- **GIVEN** a successful merge in `pr-auto` mode
- **WHEN** re-composing the remaining `layered` features against new `main` produces a conflict
- **THEN** iris does NOT change the dogfood branch's SHA, does NOT overwrite the manifest, and returns `recompose: { attempted: true, succeeded: false, conflict: { branch: "...", message: "..." } }` so the agent can drive resolution

#### Scenario: Recompose is skipped when no dogfood manifest exists

- **GIVEN** a successful `pr-auto` ship
- **AND** no `dogfood-manifest.json` file exists for the source repo
- **WHEN** iris evaluates the re-compose step
- **THEN** iris skips it and returns `recompose: { attempted: false }`

#### Scenario: Recompose is skipped when shipped branch was not in the manifest

- **GIVEN** a successful `pr-auto` ship of a branch not present in the dogfood manifest's `layered` array
- **WHEN** iris evaluates the re-compose step
- **THEN** iris fetches new main but does NOT touch the dogfood branch or manifest, and returns `recompose: { attempted: false, ... }` with a structured warning

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris ship-feature --branch <name> --via pr` from any shell
- **THEN** the same `verbs.ShipFeature` Go function executes and prints the structured result as pretty-printed JSON
