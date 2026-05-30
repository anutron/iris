## Context

`.iris.toml` is the project-wide config file iris reads on every reload. The `add-dogfood-and-ship-verbs` change introduced `dogfood_branch` (which branch a developer composes feature branches onto for local QA) and `ship_ci_timeout_seconds` (how long `ship_feature --via pr-auto` waits for CI). Both are checked in, which means:

1. Every developer in the repo inherits the same value, even though the dogfood workflow is inherently personal.
2. Some developers might not want to use dogfood at all; presence of the field still forces opt-in.
3. Adding any future personal-workflow field has the same problem and worsens the deploy hazard surfaced in task #10 (the running daemon must understand every field in the on-disk shared toml).

The well-trodden fix is a two-file pattern with the local file gitignored. `.env` / `.env.local`, `next.config.js` / `next.config.local.js`, `.npmrc` (project) vs `~/.npmrc` (user) — all the same shape. We adopt it here.

## Goals / Non-Goals

**Goals:**

- Per-developer config lives in `.iris.local.toml`, gitignored, never travels in the repo.
- Project-wide config lives in `.iris.toml`, checked in, identical for every developer.
- Existing repos that have `dogfood_branch` in `.iris.toml` continue to work with a warning — no hard break.
- `iris:status` can tell a human (or another agent) where each config value came from.
- The taxonomy is enforced — putting a per-developer field in shared config produces a warning; putting a shared field in local config produces a warning.

**Non-Goals:**

- No new fields. This change is structural only.
- No support for arbitrary user-home overrides (e.g., `~/.iris/<repo>.toml`). Two files at the repo root is sufficient; we don't need a search path.
- No environment-variable overrides. Out of scope.
- No support for per-branch or per-task overrides. The local file applies project-wide for that developer.

## Decisions

### Two files at the repo root

**Decision:** `.iris.toml` (checked in) + `.iris.local.toml` (gitignored), both at the repo root. Local file is optional.

**Rationale:** Familiar shape, no search path, no precedence ambiguity. The local file's existence is the opt-in signal — repos that don't use the feature simply don't have one.

**Alternatives considered:**

- *User-home override (`~/.iris/<repo-id>.toml`)* — more flexible but requires repo identity (path? remote URL?) and creates ambiguity about which file wins when both exist. Rejected; not worth the complexity.
- *Environment variables* — invisible from a status surface, hard to validate, hard to introspect. Rejected.
- *Single file with sections like `[local]`* — gitignoring half a file is awkward; the diff between local-on and local-off would always show up. Rejected.

### Field taxonomy is explicit

**Decision:** Each field in the `IrisToml` struct is tagged with its kind: `shared` or `local`. The loader enforces this — a shared field in the local file produces a warning; a local field in the shared file produces a warning. Both remain readable (graceful migration).

**Rationale:** Without an explicit taxonomy, the codebase has no way to know whether a future field belongs in shared or local. With it, adding a new field forces the author to declare its kind, and the validator catches misplacement.

Initial classification:

- **Shared:** `schema_version`, `default_branch`, `[build]`, `[restart]`, `[pre_flight]`, `[verify]`, `[post_merge]`
- **Local:** `dogfood_branch`, `ship_ci_timeout_seconds`

**Alternatives considered:**

- *Implicit by field-set membership* — error-prone; no compile-time check that a new field gets classified. Rejected.
- *Two separate Go structs (`SharedConfig` and `LocalConfig`)* — over-engineered for two local fields today; would require refactoring every consumer. The unified struct with a tag is simpler.

### Merge order: local wins

**Decision:** When both files set a per-developer field, `.iris.local.toml` wins. For fields tagged `shared`, only `.iris.toml`'s value is used; the local file's value is ignored with a warning.

**Rationale:** Local override is the whole point. A shared field that differs between the two files should be a warning (someone made a mistake), not a silent override — those values should be identical or only in the shared file.

### Migration: warnings, not errors

**Decision:** A shared `.iris.toml` containing local-tagged fields produces a `local_field_in_shared_config` warning per field, but parsing succeeds and the value is honored as if it were in the local file. Validation reports the warning; iris doesn't refuse to start.

**Rationale:** Existing repos (notably iris itself, which just shipped `dogfood_branch` to its own `.iris.toml`) need to migrate. Hard-failing breaks every dev mid-flight. The warning gives explicit migration guidance and the field still works while the developer moves it.

After enough time has passed, a future change can promote the warning to an error. Out of scope for this change.

### Status surfaces sources

**Decision:** `iris:status` adds `config_sources: map<string, "shared" | "local">` reporting which file each field came from. Fields the loader couldn't read (missing file, unset field) don't appear.

**Rationale:** Without this, the human or agent reading status has no way to tell whether `dogfood_branch = "dev"` was checked in for everyone or set in their local file. The motivation for this whole change is taxonomy — the status surface should make the taxonomy visible.

### Manifest carries a 1-deep `previous_manifest`

**Decision:** When `set_dogfood` overwrites the manifest file, it embeds the prior manifest's full contents under `previous_manifest` in the new manifest. The embedded value is itself a manifest, but its OWN `previous_manifest` field is stripped to prevent unbounded recursion. So at any moment a developer sees: current state + one step back.

**Rationale:** A common workflow question is "what was on dev before this compose?" — answering it from the audit log requires reading SHA history and reconstructing what each commit represented. The manifest already has the structured representation. Capturing one level of memory makes the question instantly answerable, at a tiny storage cost (one extra manifest per write). More history than that is a real feature (named compositions, garbage collection, query interface) and is deferred.

**Alternatives considered:**

- *Full history chain* — recursive `previous_manifest` of arbitrary depth. Rejected: unbounded growth, requires GC.
- *Separate history file (`dogfood-history.jsonl`)* — proper memory but bigger scope; defer until the full memory feature lands.
- *Audit log lookup* — already exists, but unstructured. The manifest's structured `layered` array is what we want; reconstructing it from commit messages would be brittle.

### `iris:set_local_config` writes `.iris.local.toml` from a sandboxed worker

**Decision:** Add a new verb `iris:set_local_config(fields)` that writes (or merges into) `.iris.local.toml` at the source repo root. Workers in argus sandboxes can't write outside their worktree, so without this verb a worker would have no way to bootstrap a dogfood workflow for itself. Read-side already covered by `iris:status` (returns merged config + provenance).

**Signature:**

```
iris:set_local_config(task_id, fields, delete) -> { written, path, resolved, warnings }
```

- `fields` — partial object of local-tagged field names to set. Values are taken literally.
- `delete` — optional array of field names to remove from `.iris.local.toml` (gives workers a way to unset without ambiguous sentinel values).
- Merge semantics: read existing file (silently treat missing as empty), apply removals first, then apply sets. Write the result back atomically (tmp + rename).
- Taxonomy enforcement: any field name in `fields` or `delete` whose `FieldKind(name) != "local"` is refused with `field_not_local` error. No writes occur.
- Validation: each field's value runs through the same per-field validator the loader uses (`dogfood_branch` is a valid git ref name, `ship_ci_timeout_seconds >= 0`, etc.). Failures refuse with `invalid_value` error. No writes occur.
- Concurrency: acquire the source-repo lock for the read-modify-write.
- Idempotency: setting fields to the same values they already hold succeeds silently.

**Rationale:** Workers should be able to set up their own dogfood end-to-end without escaping the sandbox. This is the symmetric write counterpart to `iris:status`'s read surface. Keeping the verb narrow (refuses shared fields, validates values) means it can never be used to corrupt the daemon's running state — only to set per-developer workflow knobs.

**Alternatives considered:**

- *Generic `iris:write_file(path, content)`* — too broad; lets a worker write arbitrary files outside its worktree. Rejected on principle.
- *One verb per field (`iris:set_dogfood_branch`, `iris:set_ship_timeout`)* — verbose, doesn't scale as new local-tagged fields appear. Rejected.
- *Have `set_dogfood` write `.iris.local.toml` as a side effect* — conflates two responsibilities (composing dev vs configuring iris). Rejected.

### `.gitignore` is owned by the consuming repo, not iris

**Decision:** iris's `setup.sh` adds `.iris.local.toml` to the repo's `.gitignore` if missing. iris does NOT enforce the gitignore at runtime.

**Rationale:** A developer who deliberately wants `.iris.local.toml` checked in (for whatever reason) shouldn't be blocked by iris. The setup script provides the convention; the rest is on the developer.

## Risks / Trade-offs

- **[Risk]** Developer sets `dogfood_branch` only in `.iris.toml` and never migrates. → Mitigation: validate-config and status both surface the warning; README documents the new pattern; warnings have explicit migration text.

- **[Risk]** Two configs at the repo root invites confusion ("which file do I edit for X?"). → Mitigation: validate-config reports the taxonomy violation; README has a quick-reference table; iris's own repo serves as the example.

- **[Trade-off]** Adding `config_sources` to status changes the result shape. Not a breaking change (additive field) but consumers that destructure the response need to know about it. → Acceptable — status is meant to surface state.

- **[Trade-off]** The merge is purely additive — no deep-merge of nested tables. If a future shared field becomes nested and we want partial local overrides, the design would need extension. → Accept now; YAGNI for the two flat fields we have.

## Migration Plan

1. Ship this change.
2. iris's own `.iris.toml` gets `dogfood_branch` removed in the same change. Developers using iris locally create their own `.iris.local.toml` with `dogfood_branch = "dev"` (or whatever).
3. iris's `setup.sh` is updated to: (a) add `.iris.local.toml` to repo gitignore, (b) prompt for a dogfood_branch and write a starter `.iris.local.toml`.
4. No deploy hazard this time: the new shared schema only REMOVES a field, and old binaries that still expect `dogfood_branch` in the shared file just won't find it (treated as unset, no dogfood enabled for iris-on-self until the developer creates `.iris.local.toml`).

## Open Questions

- **Naming.** `.iris.local.toml` is the recommendation, but `iris.local.toml` (no leading dot) or `.iris.user.toml` are also reasonable. Sticking with `.iris.local.toml` unless someone has a strong opinion.
- **Should `setup.sh` write a starter `.iris.local.toml`?** Probably yes, interactively, with a sensible default of `dogfood_branch = "dev"`. Confirmation in implementation.
- **`config_sources` field on absent fields.** A field that's unset in both files should probably be omitted from `config_sources` (rather than `"none"`). Default to omit.
