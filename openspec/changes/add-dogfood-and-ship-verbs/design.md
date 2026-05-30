## Context

Iris already brokers every host-repo write the agent needs: `iris_push`, `iris_cherry_pick`, `iris_branch_create`, `iris_checkout`, `iris_gh_pr_*`, `iris_merge_to_master`, `iris_fetch`. The sandboxed agent cannot write directly to the source repo, so Iris is the trusted boundary for every mutation.

Two recurring developer motions have no first-class verb:

1. **Dogfood compose** — "I have main + feature branches F1, F2, F3. Stage main + F2 + F3 on a dev branch, rebuild, restart the service." Today the agent runs an ad-hoc sequence of `branch_create`/`cherry_pick`/`checkout` and the dogfood state is implicit (whatever happens to be on the dev branch). There's no persisted record of which features compose it, so `iris_status` can't communicate "what's running."

2. **Ship a feature** — "F2 is approved; land it on `origin/main`." For Thanx repos this means push + PR + wait for review + merge. For personal repos the user wants the same flow with auto-approve + auto-merge — same audit trail and CI gate, no extra ceremony. After shipping, the dogfood branch should be re-composed without the shipped feature.

The invariant we want to preserve: **local `main` is read-only relative to `origin/main`** — it only moves via `git fetch`. That keeps argus's fork-from-local-main behavior reproducible and eliminates the "unpushed main commits" failure mode entirely.

## Goals / Non-Goals

**Goals:**
- Provide a single primitive — "move the dogfood branch to this SHA + record what's there" — that any composition strategy (cherry-pick, merge, rebase, octopus) can target.
- Persist a structured manifest the agent supplies, so `iris_status` can tell humans and downstream agents what's running.
- Provide a single ship motion (`pr` for review, `pr-auto` for skip-review) that always goes through GitHub.
- Keep `main` immutable from the worker's perspective. No verb writes to local `main`; only `iris_fetch` does.
- Opt-in per repo via `.iris.toml`. Repos without `dogfood_branch` set are unaffected.

**Non-Goals:**
- Iris does **not** perform composition. Cherry-pick vs merge vs rebase, base selection, conflict resolution — all live in the agent (typically driven by a project-side skill).
- No direct-push-to-main path. The original `iris_push_main` idea is dropped; `iris_push`'s default-branch refusal remains.
- No "octopus merge" or any multi-branch atomic primitive in Iris. The agent builds the rollup somewhere it can write (its worktree, a scratch branch) and hands a SHA to Iris.
- No automated conflict resolution. Conflicts during ship's re-compose phase surface to the agent as structured errors, not as Iris best-effort guesses.

## Decisions

### Iris is dumb, agent is smart

**Decision:** `iris_set_dogfood(sha, manifest)` takes a pre-built SHA and a manifest. Iris hard-resets the configured dogfood branch to that SHA, persists the manifest, runs the existing build/restart machinery, and returns.

**Rationale:** Composition policy varies per project (cherry-pick vs merge), per situation (do we want F1's tests included?), and is inherently interactive when conflicts arise. The agent has the context; Iris doesn't. By limiting Iris to "atomically set this branch to this SHA," we get a single primitive that supports dogfood, "test this exact PR," "roll back to yesterday," and any future use case — without teaching Iris about each.

**Alternatives considered:**
- *Iris accepts a list of branches and does the merge.* Rejected — bakes composition policy into Iris and turns conflicts into error states instead of conversations.
- *Iris exposes lower-level primitives (`iris_reset_branch` + `iris_save_manifest`) and the agent composes them.* Rejected — non-atomic, and `set_dogfood` is the actual workflow boundary. Two verbs that always get called together should be one verb.

### Manifest is structured, not opaque

**Decision:** Manifest shape:

```
{
  base: { ref: "main", sha: "abc123..." },
  layered: [
    { name: "F2", sha: "...", applied: "cherry-pick" },
    { name: "F3", sha: "...", applied: "cherry-pick" }
  ],
  note: "optional free-text from the agent"
}
```

**Rationale:** A future LLM reading `iris_status` should be able to reason about what's on dogfood (e.g., "shipping F2 means re-composing with just F3"). Opaque strings prevent that. Structure also lets the ship verb mechanically drop a shipped feature from the manifest.

The `applied` field is descriptive only — Iris doesn't validate it. The agent records "this is how I built it" for future reference.

### Manifest persistence: alongside audit log

**Decision:** Persist the manifest as `dogfood-manifest.json` next to the existing audit log under the iris state directory for that source repo. Single file, overwritten on each `set_dogfood`.

**Rationale:** Iris already has a per-source-repo state directory for audit logs. One more file there is the path of least surprise. We don't need history — only the current manifest matters; the audit log already records the SHA progression.

**Alternatives considered:**
- *Embed in git via notes or trailers.* Rejected — complicates parsing and couples the manifest to git's mutability.
- *History of manifests for rollback.* Rejected — YAGNI. If you need history, look at the audit log's SHAs.

### Ship is an orchestrator, not a primitive

**Decision:** `iris_ship_feature(branch, via)` composes existing verbs (`iris_push`, `iris_gh_pr_create`, optionally GitHub merge API calls, `iris_fetch`) into one motion, plus the post-ship dogfood re-compose.

**Rationale:** Each step already exists. Bundling them lets the agent say "ship F2" without orchestrating five tool calls. Keeping the underlying verbs separate preserves them for cases where the agent wants finer control.

**Two via modes:**
- `pr` — push branch + open PR. Stop. Worker comes back to it after review.
- `pr-auto` — push branch + open PR + approve PR + merge PR + fetch + re-compose dogfood.

No `direct-merge` mode. The only "advantages" of skipping the PR (slightly faster, no CI gate) are anti-features for serious work; `pr-auto` gives the same outcome with audit trail and CI.

### Post-ship dogfood re-compose

**Decision:** After a successful `pr-auto` (or after the user later merges a `pr`-opened PR and calls a future `iris_post_merge_recompose` verb — out of scope for this change), Iris fetches new main and re-applies the manifest **with the shipped feature dropped**.

If the re-apply succeeds (no conflicts), the new SHA + updated manifest replace the prior dogfood state. If any layered branch fails to re-apply cleanly, Iris returns a structured error naming the conflicting branch and leaves the dogfood branch on the previous SHA. The agent then drives conflict resolution and calls `iris_set_dogfood` again with the resolved SHA.

**Rationale:** Mechanical re-compose is the common case ("F1 shipped, F2 and F3 still apply cleanly to new main"); making Iris do it saves a round-trip. Conflicts are the conversation case; surfacing them lets the agent + user decide.

**Scope clarification:** For the `pr` (non-auto) mode this change does NOT auto-trigger re-compose — the worker may not be around when the PR is finally merged by a teammate. A follow-on change can add a `post_merge_recompose` hook or a manual verb if needed.

### Opt-in per repo

**Decision:** `dogfood_branch` is a new string field in `.iris.toml`. Empty/unset = the verbs refuse with a structured error.

**Rationale:** Most Thanx repos don't want this surface. Personal repos opt in once. Mirrors the existing `default_branch` field's optionality model.

### `iris_push` unchanged

**Decision:** Keep the existing default-branch refusal in `iris_push`. Do not add a flag to relax it.

**Rationale:** In the new model, the worker never pushes main. If you find yourself wanting to push main, something is wrong upstream and an error is the correct outcome.

## Risks / Trade-offs

- **[Risk]** The agent ships a SHA that doesn't actually contain the manifest's layered features (lying or buggy). → Mitigation: `set_dogfood` is descriptive; Iris doesn't validate that `layered[i].sha` is reachable from the supplied SHA. We accept this — Iris is not a policy enforcer. The manifest is for communication, not verification. (A future enhancement could spot-check ancestry.)

- **[Risk]** `pr-auto` merges a PR with failing CI if branch protection isn't set. → Mitigation: `pr-auto` SHALL wait for CI checks to pass (or be explicitly absent) before approving. Hard-fail if checks are failing.

- **[Risk]** Re-compose after ship surfaces a conflict and the dogfood branch is now stuck on stale main. → Mitigation: dogfood branch state is unchanged on re-compose failure. The user sees the conflict, the dogfood keeps running the prior (now-slightly-stale) build. They redo the compose when ready.

- **[Trade-off]** Manifest is a per-process write — no transaction across the git reset and the manifest file. A crash between the two leaves them inconsistent. → Mitigation: write manifest first (durable), then reset branch. If the reset fails, the manifest is ahead; on next `iris_status`, drift is visible. Acceptable for a local dev tool.

- **[Trade-off]** No history of manifests. If you want to know "what was dogfooded last Tuesday," you'd have to derive it from the audit log's SHA progression and git history. Acceptable — the audit log is the canonical record.

## Migration Plan

No migration needed. Existing repos without `dogfood_branch` in `.iris.toml` are unaffected. Iris itself adopts the feature by adding `dogfood_branch = "dev"` (or similar) to its own `.iris.toml` once the change ships.

## Open Questions

- **Naming.** `iris_set_dogfood` ties the verb to one workflow. Alternatives: `iris_pin_dev_branch`, `iris_set_iteration_branch`. Settling on `set_dogfood` for now because it names the workflow it serves; happy to rename if a more general use case emerges before implementation.
- **CI check polling.** For `pr-auto`, how long does Iris wait for checks? Configurable timeout in `.iris.toml`, or a sensible default (e.g., 10 min)? Lean toward a `.iris.toml` field like `ship_ci_timeout_seconds` with a default of 600.
- **PR merge style.** Squash, rebase, merge commit? Probably `squash` by default with an override via `.iris.toml` or per-call. Defer to implementation.
