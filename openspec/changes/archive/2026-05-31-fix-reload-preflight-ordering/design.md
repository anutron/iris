## Context

`iris:reload` upgrades a running daemon by pulling the default branch, building, and restarting. `iris:publish` does the same after fast-forwarding (or `--reset`-ing) the source repo to an argus worktree's HEAD. Both load and validate `.iris.toml` during pre-flight, **before** the repo reaches the state that the new binary will run against.

The validation is performed in-process by the **currently running** (old) daemon binary. Its TOML decoder treats any field it does not recognise as an "unknown field" validation error (`internal/config/iris_toml.go`, `meta.Undecoded()` → `ValidationError`). So when an additive schema change has already landed in the on-disk `.iris.toml`, the old binary refuses pre-flight and never reaches the build that would teach it the new field. This is a chicken-and-egg deadlock — surfaced concretely shipping `add-dogfood-and-ship-verbs`, and called out as the residual hazard in `add-iris-local-toml-overlay`'s design.

Current reload order (`Reload`): resolve → self/cross → CLI-self refusal → **load+validate `.iris.toml`** → clean-tree/branch/origin checks → lock → `[pre_flight]` hook → **pull** → build → restart → verify → audit.

Current publish order (`Publish`): resolve → clean worktree + clean source → **load+validate source `.iris.toml`** → branch-match check → lock → ff-merge/reset → push → build → restart → audit.

Constraints carried over from the existing specs:
- A malformed or schema-invalid `.iris.toml` MUST still fail the reload — just at the correct (later) step.
- The failure shape stays structured (`valid: false` / `errors: [...]`).
- The audit log must record `pre_pull_sha`, `post_pull_sha`, and the validation outcome at the point it is decided.

## Goals / Non-Goals

**Goals:**
- Make an additive `.iris.toml` field deployable in a single `iris:reload` (or `iris:publish`) with no bootstrap commits and no manual binary swap.
- Validate the configuration that will **actually run** after build+restart, not stale pre-pull state.
- Preserve every existing refusal (missing file, malformed TOML, schema/mechanism validation) — only the *timing* and the unknown-field *severity* change.

**Non-Goals:**
- No change to the `.iris.toml` on-disk shape or to any field.
- No relaxation of `iris:validate_config` — authoring-time validation stays strict so typos are still caught.
- No change to `schema_version` handling — a major-version bump remains a hard refusal.
- No automatic rollback if the new binary turns out to reject its own config after restart (out of scope; verify hooks already cover post-restart health for cross-reload).

## Decisions

### Decision 1: Pull, then validate (reload)

`iris:reload` fetches + `merge --ff-only`s the default branch **before** loading and validating `.iris.toml`. The post-pull file is the exact config the rebuilt binary will consume, so validating it (rather than the pre-pull file) validates the truth.

Because the pull target (`default_branch`) can itself be overridden in `.iris.toml`, a single deferred load is impossible — we need *some* config before the fetch. The resolution: a **lenient pre-pull peek** that decodes the on-disk `.iris.toml` solely to read `default_branch`, swallowing every error (missing file, malformed TOML, unknown fields all yield `""`). On `""` the existing `resolveDefaultBranch` fallback (git `origin/HEAD` → `main` + warning) applies. No refusal is based on the pre-pull file.

The authoritative load+validate then runs **after** the pull. Missing-file, malformed-TOML, and schema-validation refusals fire here. The `[pre_flight]` hook also moves here (it reads `doc.PreFlight` from the post-pull doc and now gates against the freshly-pulled tree, which is more correct — it can veto the new code).

New reload order: resolve → self/cross → CLI-self refusal → **lenient peek for `default_branch`** → clean-tree/branch/origin checks → lock → **pull (fetch + ff-merge)** → **load+validate post-pull `.iris.toml` (tolerant of unknown fields)** → `[pre_flight]` hook → build → restart → verify → audit.

**Alternatives considered:**
- *Validate twice (pre-pull strict + post-pull strict).* Rejected — the pre-pull validation is exactly the one that breaks additive deploys; running it at all reintroduces the bug.
- *Keep validation pre-pull but skip it when `pulled` will change HEAD.* Rejected — fragile and still validates stale config in the `no_pull` path.

### Decision 2: Validate the worktree config (publish)

`iris:publish` does not pull from origin; it sets the source repo to the **worktree's** HEAD (`merge --ff-only <wtSHA>` or `reset --hard <wtSHA>`). The config that will run after the update is therefore the worktree's `.iris.toml` at `<wtSHA>` — and the worktree is checked clean in pre-flight, so its working-tree `.iris.toml` already equals that. We validate `filepath.Join(worktreePath, ".iris.toml")` (the config being published) instead of the source repo's stale pre-publish `.iris.toml`.

This validates the post-update truth **without** reordering past the mutation — strictly safer than reload's reorder, because no destructive `reset --hard` happens before the config is judged. The mechanism legitimately differs from reload (reload's truth lives only on origin until pulled; publish's truth is already in the worktree) but the principle is identical: validate what will run.

**Alternatives considered:**
- *Reorder publish to validate after the ff-merge/reset (mirror reload exactly).* Rejected — would `reset --hard` the source repo and then potentially fail validation, leaving the operator's source in a hard-reset state on a refusal. Validating the worktree upfront is the same answer, safely.

### Decision 3: Tolerant decode mode (unknown fields → warnings)

A new decode mode downgrades **unknown-field** errors to warnings. Reload validates the *post-pull* file with the *old* binary's decoder; an additive field freshly arrived from origin would read as "unknown field" and refuse — so the reorder is only deployable in one shot if unknown fields are tolerated. Publish has the same shape (worktree adds a field the old daemon does not know).

API: add `LoadMode{ TolerateUnknownFields bool }` plus `LoadIrisTomlMode(path, isSelf, mode) (*IrisToml, []ValidationError, []string, error)` and `DecodeIrisTomlMode(data, path, isSelf, mode) (...)`. The existing `LoadIrisToml`/`DecodeIrisToml` keep their signatures and delegate with `LoadMode{}` (tolerate=false), so every other caller (`validate_config`, `set_dogfood`, `ship_feature`, `merge_to_master`, `status`) is byte-for-byte unchanged. When `TolerateUnknownFields` is true, each `meta.Undecoded()` key becomes a warning string instead of a `ValidationError`; reload/publish append these to the result `warnings` and the audit entry.

What stays strict even in tolerant mode:
- **`schema_version`** mismatch — a bumped major version deliberately signals "old binary, refuse." Additive fields are forward-compatible *within* a version and never bump it; breaking changes bump it and are correctly refused. This is the clean safety boundary that makes tolerating unknown fields sound.
- **Malformed TOML** (syntax error) — a parse failure, not an unknown field; stays a hard `ValidationError`.
- All other cross-validation (required fields, restart-mechanism field exclusivity, exit_code self-only, etc.).

**Alternatives considered:**
- *Tolerate unknown fields globally (including `validate_config`).* Rejected — authoring-time validation must catch typos; silently warning on `[buld]` would let a misconfigured deploy through.
- *Tolerate via a per-field allowlist.* Rejected — the whole point is forward compatibility with fields this binary has never heard of; an allowlist cannot enumerate the future.

### Decision 4: Interaction with the dogfood (dev-branch) rebuild path

`iris:reload` always targets the **default branch** (it fetches `origin/<default-branch>` and refuses if HEAD is not on it). A developer using the dogfood workflow rebuilds off their **dev branch** via `iris:set_dogfood`, not via `iris:reload` directly. `set_dogfood` composes a SHA, repositions the dogfood ref, and then calls `Reload` with `no_pull = true` (`internal/verbs/set_dogfood.go`) to drive the build + restart.

Two consequences for this change, both desirable:
- The pull-then-validate **reorder** is a harmless no-op on the `no_pull` path: no fetch/ff-merge happens, so `post_pull_sha == pre_pull_sha` and validation runs against the same on-disk config that will be built — exactly as before, just sequenced after the (skipped) pull step.
- The forward-compatible **unknown-field tolerance** is inherited by `set_dogfood` for free, because it routes through `Reload`. So composing a dogfood SHA that introduces an additive `.iris.toml` field also deploys in a single `set_dogfood` call — the same one-shot property we give the reload-off-main path.

This means the fix covers both motions: "rebuild off main" (`reload`) and "rebuild off dev" (`set_dogfood` → `reload --no-pull`). No separate `set_dogfood` change is required; the benefit falls out of routing through the shared `Reload` helper.

## Risks / Trade-offs

- **[Pre-pull `default_branch` override changes across the pull]** → The fetch uses the pre-pull override; if a commit on origin both moves the daemon and changes `default_branch`, the first reload fetches the old target. Mitigation: rare, self-correcting on the next reload, and `default_branch` overrides are near-static. The git `origin/HEAD` fallback covers the common (no-override) case correctly regardless.
- **[`[pre_flight]` hook now runs after the pull]** → On hook failure the repo has already fast-forwarded. Mitigation: the ff-merge only advances local `main` to `origin/main` (the dogfood invariant keeps local `main` read-only except via fetch), which is the desired state anyway; nothing is built or restarted. The spec scenario is updated to "does NOT build or restart" (dropping "does NOT pull").
- **[Tolerating unknown fields hides typos at deploy time]** → A misspelled field (`[buld]`) would warn instead of error during reload. Mitigation: `iris:validate_config` stays strict and is the authoring-time gate; the deploy-time warning is surfaced in the result and audit log; and the alternative (refusing every forward-compatible field) is the bug we are fixing. Net: typo protection moves to authoring time where it belongs.
- **[Publish validates worktree, not source]** → If the worktree and source `.iris.toml` somehow differ from what the post-update source will be (they cannot, given a clean worktree and ff/reset to its HEAD), validation could mislead. Mitigation: the clean-worktree pre-flight check guarantees worktree working tree == `<wtSHA>` == post-update source.

## Migration Plan

Ship the decoder mode and the two verb reorderings together in one change. The change is to the decoder/ordering, **not** to the `.iris.toml` shape, so the deploy of *this* change is itself unaffected by the old hazard (no new field is introduced). After it lands and the daemon restarts onto the new binary, the next additive schema change deploys in a single `iris:reload`. No rollback concerns beyond a normal binary revert.
