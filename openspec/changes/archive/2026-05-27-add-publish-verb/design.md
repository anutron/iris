## Context

`iris:reload` (v1.1) updates a source repo's default branch from origin, builds, and restarts. The dogfood inner loop is now: edit in an argus worktree, commit, push (or not), then trigger reload. But reload only handles the default-branch path and only pulls from origin – it cannot reflect a worktree-local commit onto the source repo's working tree.

Operators have been working around this by `cd`-ing into the source repo, manually `git fetch <worktree>` / `git reset`, then invoking reload. `iris:publish` is the single verb that collapses this into "from a worktree, sync the source repo to my HEAD, then build+restart".

Existing assets we lean on:

- `verbs.Resolve` (returns worktree path, source repo path, current branch, with canonicalization + allowlist check)
- `lockSourceRepo` (per-source-repo mutex shared with reload / push / merge-to-master)
- `verbs.Push` style git plumbing in `internal/verbs/push.go` (push with force-with-lease, default-branch refusal)
- `runBuildBlock`, `dispatchRestart`, `AppendAuditBestEffort` from `internal/verbs/reload.go`
- `.iris.toml` loader and validation from `internal/config`

## Goals / Non-Goals

**Goals:**

- One verb, one flag set, one round-trip: `iris publish <task_id> [--branch=X] [--push] [--reset]`.
- Reuse reload's build+restart code path verbatim; no parallel implementation of restart dispatch.
- Safe by default: fast-forward only; explicit opt-in for `--reset` (hard reset) and `--push`.
- Audit-log every publish in the same JSONL as reload, distinguished by `mode: "publish"`.
- Pre-flight refusals match reload's posture: clean worktree, clean source repo, `.iris.toml` valid, source repo allowlisted, target branch == current source repo branch.

**Non-Goals:**

- No new `.iris.toml` schema. The target branch is per-call, not per-repo configuration.
- No `--no-rebuild` / `--no-restart` knobs. Publish IS update-and-rebuild-and-restart.
- No self-hosting / no-task-id discovery. A publish always originates in a worktree, so `task_id` is required.
- No force-push flag in v1.2. The `--push` step matches v1.0 `iris:push`'s existing guardrails (refuses default branch, no --force-with-lease unless the existing `iris:push` opt-in is plumbed through later).
- No "checkout the target branch first" behavior. v1.2 refuses if the target branch isn't the source repo's current HEAD. (A future v1.3 could add a `--checkout` flag.)

## Decisions

**D1. Target branch is a per-call flag, not a `.iris.toml` field.** Different worktrees of the same source repo may target different branches; the source repo's `.iris.toml` doesn't know which branch the operator wants today.

**Alternatives considered:** Adding `[publish] default_branch = "..."` to `.iris.toml`. Rejected because (a) it's not a property of the source repo, it's a property of the call, and (b) the typical case is "publish to whatever the source repo is on right now", which the flag default already encodes.

**D2. Default is ff-only; `--reset` opts into hard reset.** Bold (destructive) modes are explicit.

**Alternatives considered:** Default to `--reset`. Rejected because the operator's mental model in git is "ff is safe, reset is risky" – matching git's mental model keeps the verb predictable.

**D3. Always rebuild and restart.** No `--no-rebuild` / `--no-restart` flags. The verb IS update-and-rebuild-and-restart; if the operator only wants the git update, they can use git directly.

**Alternatives considered:** Add flags to suppress steps. Rejected as YAGNI – we can add them later if a use case emerges. Reload's surface doesn't have these flags either.

**D4. v1.2 constraint: target branch must equal source repo's current HEAD.** Refuse otherwise.

**Why:** Atomically changing both the ref and the working tree (a `git checkout` followed by an update) is a more delicate sequence than ff-update of the currently-checked-out branch. Defer that complexity to a future v1.3 `--checkout` flag once a real use case exists. For v1.2, "update the branch the source repo is currently on" covers the common dogfood loop.

**Alternatives considered:** Auto-checkout the target branch. Rejected for v1.2 – three failure modes (dirty working tree, branch doesn't exist, branch exists but with conflicting changes) each need their own UX, and we don't need the surface area yet.

**D5. Reuse `runBuildBlock`, `dispatchRestart`, `writeAudit` from `reload.go`.** Single source of truth for build/restart logic.

**Why:** Reload has already absorbed the operational learnings around build timeouts, process-group kill, mechanism dispatch, audit-log shape. Forking that code into publish would diverge over time.

**How:** Both verbs live in `package verbs`, so the existing unexported helpers are already reachable. No need to export anything.

**D6. Audit entries share the same JSONL file but distinguish via `mode: "publish"`.** `AuditEntry.Mode` is a free-form string today (reload writes `"self"` or `"cross"`), so adding a third value is a no-op schema change.

**Why:** Operators already grep `~/.iris/reload-history.jsonl` via `iris ls` / `iris status`; adding publish to the same file keeps a unified daemon history. `iris ls` will naturally show publishes alongside reloads with no UI change required.

**D7. `task_id` is always required.** Not a self-hosting verb.

**Why:** "Publish from a worktree" inherently has a worktree. There is no useful `iris publish` with no arguments – that would be `iris reload`.

**D8. `--push` matches v1.0 `iris:push`'s guardrails.** Refuses default branch, no force-push escape hatch in v1.2.

**Why:** Don't widen the push surface in publish beyond what `iris:push` already does. If the worktree's branch is the default branch, the operator should be using `iris:reload`'s pull path instead. (In v1.2, the target branch constraint D4 already prevents this combination from being attempted.)

## Risks / Trade-offs

- [Risk] Operator on the source repo expects the working tree to be at a specific commit and `--reset` clobbers it → Mitigation: pre-flight refuses dirty working tree in the source repo, so we never overwrite uncommitted work. Committed-but-unpushed work IS clobbered by `--reset` (that's the point of the flag), and the audit entry records pre/post SHAs so it's recoverable.

- [Risk] Build or restart fails mid-publish, leaving the source repo on the worktree's HEAD but the daemon unrunnable → Mitigation: same risk as reload; mitigated by the audit log (operator can see the SHA to revert to) and by the build always running before restart (build failure means no restart, no broken daemon).

- [Risk] Worktree SHA isn't an ancestor of the target branch and the operator uses default (ff-only) → Mitigation: clear error message naming both SHAs and pointing at `--reset`. Same UX as reload's divergent-history error.

- [Risk] Per-source-repo lock contention with reload on the same source repo → Acceptable: serialization is the point of the lock; a publish blocks a concurrent reload (and vice versa) until the first completes.

- [Risk] `--push` succeeds but build fails → The remote ref still advances but the local daemon doesn't update. Acceptable in v1.2: the operator wanted both, the partial outcome is recoverable by running publish again after fixing the build, and the audit log records the failure.

## Migration Plan

No migration. Pure addition. Daemon restart picks up the new verb registration.

## Open Questions

None at design lock. The seven decisions above were pre-discussed with the user before scaffolding.
