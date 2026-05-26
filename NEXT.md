# NEXT – iris build continuation

This is the handoff doc for a fresh Claude Code session picking up the iris build overnight. Read it cover-to-cover before doing anything else.

## Mission

Complete the iris plugin v1 verb set, run ralph-review on the assembled branch, run spec-audit, deliver to Aaron for morning review.

Aaron's literal instruction:

> When I return, either you will have completed all of the work or you will have a few handful of questions where you had low confidence at a fork in the road that needs my input.

## Current state

- Repo: `~/Development/Personal/iris` (private, `anutron/iris` on GitHub)
- Branch: `argus/handoff-bootstrap-iris-plugin` – 4 commits ahead of `origin/main`
- PR: #1 (https://github.com/anutron/iris/pull/1) – open, not merged (cannot merge until iris itself exists to merge it; everything stacks on this branch)
- This worktree (the orchestrator's cwd): `/Users/aaron/.argus/worktrees/Iris/handoff-bootstrap-iris-plugin`
- Canonical iris repo: `/Users/aaron/Development/Personal/iris`
- All commits pushed
- `make test` passes under `-race`, `go vet ./...` clean, `openspec validate --all --strict` passes

## What's built

- Plugin scaffold: `cmd/iris`, `internal/{argus,mcp,verbs,config,daemon}`
- One verb end-to-end: `iris:merge_to_master`
- Argus client: port discovery, project list, task fetch, MCP tool registration, recovery loop (Watcher + RecoverFunc + linkstate)
- MCP server: bearer-token auth, 1 MiB body cap, callback handler dispatch
- `setup.sh` installer (build, install to `~/bin`, mint token, install LaunchAgent)
- Tests across `argus`, `config`, `mcp`, `verbs`
- Two delta specs: `iris-host-bridge`, `iris-merge-to-master`
- Active OpenSpec change: `bootstrap-iris-plugin` (do NOT archive – v1-verb changes stack on top, then all archive together at the end)

## What's left

| Verb | Phase | Depends on |
|------|-------|------------|
| `iris:push` | 1 (parallel) | (plumbing only) |
| `iris:gh_pr_create` | 1 (parallel) | (none) |
| `iris:gh_pr_merge` | 1 (parallel) | (none) |
| `iris:run_build` | 1 (parallel) | (none) |
| `iris:complete_task` | 2 (serial) | `merge_to_master` (done) + `push` (built in phase 1) |

Then phase 3: `/ralph-review` on the assembled branch. Then phase 4: `/spec-audit`. Then phase 5: PR description update + final push.

## Workflow

This branch (`argus/handoff-bootstrap-iris-plugin`) is the trunk. Everything lands here.

### Phase 1 – four standalone verbs in parallel

Spawn 4 Agent calls in a single message with `subagent_type="general-purpose"` and `isolation: "worktree"`. Each agent:

1. Branches from this worktree's HEAD (the Agent tool with `isolation: "worktree"` handles this).
2. Creates its own OpenSpec change folder via `openspec new change <name>` per the conventions established in `bootstrap-iris-plugin`.
3. Writes proposal, design, tasks, and delta spec (one capability per verb, e.g., `iris-push`).
4. Implements the verb following the `merge_to_master` blueprint (see "Verb blueprint" below).
5. Writes tests mirroring `internal/verbs/merge_to_master_test.go` patterns – use `stubArgus(t, sourceRepo, worktreePath)` which sets up both `/api/tasks/<id>` and `/api/projects/full` mock responses.
6. Wires handler + cobra subcommand + daemon registration + README CLI table update.
7. Commits with a clear message and returns the branch name and SHA(s).

When each agent completes, merge their branch into this branch via `git merge --no-ff <agent-branch>`. Conflicts will appear on:

- `internal/daemon/run.go` – tool registration + handler binding
- `cmd/iris/main.go` – subcommand wiring
- `README.md` – CLI command list
- `setup.sh` – completion-message hints (rare)
- `openspec/changes/bootstrap-iris-plugin/tasks.md` – section 7 follow-ups list

Resolve these serially. They're small.

### Phase 2 – `iris:complete_task` (serial after phase 1)

One agent. Implements the composite verb using `verbs.MergeToMaster` and `verbs.Push` (both built by then). Uses checkpoint pattern (see "Pre-resolved decisions" below). Mark argus task complete via `POST /api/tasks/<id>/status` with `{"status":"complete"}`. Archive via `POST /api/tasks/<id>/archive`.

### Phase 3 – `/ralph-review`

Invoke the `ralph-review` skill on the assembled branch. Apply `/doitright` to every QUESTION item with a clear long-term-correct recommendation. Park questions where the LTC pick is genuinely ambiguous.

### Phase 4 – `/spec-audit`

Invoke the `spec-audit` skill. Document gaps. Auto-fix the easy ones. Park architectural surprises.

### Phase 5 – handoff

- Update PR #1 description to reflect the expanded scope.
- Push all commits.
- Replace `NEXT.md` with `STATUS.md` summarizing what shipped, what's parked, and what Aaron should look at first.
- Append "Uncomfortable bets" entries to the bet log (see bottom of this file).

## Verb blueprint

Every verb produces these files:

- `internal/verbs/<verb>.go` – exported `<Verb>(ctx, client *argus.Client, taskID string, opts <Verb>Options) (*<Verb>Result, error)`. Calls `verbs.Resolve(ctx, client, taskID)` to get the `*ResolvedRepo`. Holds `lockSourceRepo(resolved.SourceRepo)` if mutating shared git state.
- `internal/verbs/<verb>_test.go` – happy path + every delta scenario. Reuse `stubArgus`, `setupRepoWithWorktree`, `gitRunner`, `headSHA` helpers from `merge_to_master_test.go`. Negative-path tests assert `headSHA` is unchanged.
- `internal/mcp/handler_<verb>.go` – `NewMergeToMasterHandler`-style factory returning a `Handler` that decodes the envelope input, calls the verb, marshals result with `json.MarshalIndent`, returns via `TextResponse`. On error: `ErrorResponse(fmt.Sprintf("iris:<verb>: %v", err))`.
- `cmd/iris/<verb>.go` – cobra subcommand. Loads token via `config.LoadToken`, opens `argus.NewPortsClient`, calls `ports.Ports(ctx)`, constructs `argus.New(...)`. Calls the verb function. Prints `json.MarshalIndent`'d result.
- `openspec/changes/<change-name>/`:
  - `proposal.md` – Why / What Changes / Capabilities / Impact
  - `design.md` – Context / Goals / Decisions / Risks
  - `tasks.md` – numbered checkbox tasks (all checked at commit time since the agent does the work)
  - `specs/<capability>/spec.md` – ADDED Requirements for the verb's contract

Inline edits per verb:

- `internal/daemon/run.go` – add `mcpSrv.RegisterHandler("iris_<verb>", mcp.NewVerbHandler(client))` line, add tool definition to `toolDefinitions()`.
- `cmd/iris/main.go` – add `root.AddCommand(newVerbCmd())` line.
- `README.md` – add line to CLI command list.

## Verb specs

Use these as the agent brief inputs.

### `iris:push`

- Change name: `add-push-verb`
- Args: `task_id` (string, required), `force_with_lease` (bool, default false)
- Behavior: resolve repo via `verbs.Resolve`. Refuse if `resolved.Branch == resolved.DefaultBranch` (use `verbs.DefaultBranch` to get default). Hold `lockSourceRepo(resolved.SourceRepo)`. Run `git -C <sourceRepo> push origin <branch>` with `--force-with-lease` if true.
- Returns: `{Pushed: bool, Branch: string, RemoteSHA: string}` (RemoteSHA via `git rev-parse origin/<branch>` after push)
- Scenarios: happy push, refuses default branch, refuses unknown task, force-with-lease passes when remote tracks correctly, non-ff without force-with-lease errors clearly
- Notes: push runs in the SOURCE REPO (not worktree). The worktree's branch is already a ref in the source repo via `git worktree`.

### `iris:gh_pr_create`

- Change name: `add-gh-pr-create-verb`
- Args: `task_id` (string, required), `title` (string, required), `body` (string, optional), `draft` (bool, default false)
- Behavior: resolve. Refuse if branch is default. Run `gh pr create --base <default> --head <branch> --title <T> --body <B> [--draft]` in source repo. Parse PR number and URL from stdout (gh prints `https://github.com/<owner>/<repo>/pull/<N>` on the last line on success).
- Returns: `{Number: int, URL: string}`
- Scenarios: happy create, refuses default branch, draft flag respected, empty body OK, gh not authed surfaces actionable error suggesting `gh auth login`
- Notes: gh CLI must be on PATH (verified in setup.sh? if not, document the dependency). Subprocess via `exec.CommandContext`.

### `iris:gh_pr_merge`

- Change name: `add-gh-pr-merge-verb`
- Args: `task_id` (string, required), `pr_number` (int, required), `strategy` (enum "squash"|"merge"|"rebase", default "squash")
- Behavior: resolve. Run `gh pr merge <num> --<strategy>` in source repo. Do NOT gate on CI status in v1 – caller is responsible. Document this trade in the delta.
- Returns: `{Merged: bool, Strategy: string}`
- Scenarios: happy squash-merge, rebase strategy, merge strategy, gh error surfaces
- Notes: gh's merge command returns 0 on success, non-zero on failure (CI not green, conflicts, auth). Surface the gh stderr in the error.

### `iris:run_build`

- Change name: `add-run-build-verb`
- Args: `task_id` (string, required), `target` (string, optional)
- Behavior: resolve repo. Build command resolution order:
  1. If `<worktree>/script/iris-build` exists AND is executable: run it (passing `target` as arg if set).
  2. Else if `<worktree>/Makefile` exists with a `build` target: run `make build [target]`.
  3. Else: error with actionable message naming both paths.
- Build runs in the WORKTREE (`resolved.WorktreePath`), not the source repo. Use a per-worktree mutex (separate `buildLocks` sync.Map in locks.go; keep `repoLocks` for git ops).
- Streaming: NOT in v1. Capture combined stdout/stderr via `exec.CommandContext` + `cmd.CombinedOutput()`. Return the full output in the result. Document the streaming deferral in the delta.
- Returns: `{Command: string, ExitCode: int, Output: string}`
- Scenarios: happy build via `script/iris-build`, happy build via Makefile, no build mechanism found errors, non-zero exit returns ExitCode in result plus error
- Notes: `target` for the script case is passed as `script/iris-build <target>`. For Makefile, it's the make target name.

### `iris:complete_task`

- Change name: `add-complete-task-verb`
- Args: `task_id` (string, required), `merge_strategy` (enum "no_ff"|"ff_only", default "no_ff")
- Behavior: composite with checkpoints. On any sub-step error, return immediately with the checkpoints completed so far and the error wrapped. Re-invocation can resume.
  1. Check task status; if already "complete", return success with all checkpoints. (Idempotency.)
  2. `verbs.MergeToMaster(ctx, client, taskID, MergeOptions{NoFF: strategy == "no_ff"})` – checkpoint: `merged`
  3. `verbs.Push(ctx, client, taskID, verbs.PushOptions{})` – push the now-merged default branch to origin – checkpoint: `default_branch_pushed`
  4. `git -C <sourceRepo> push origin --delete <task-branch>` – checkpoint: `remote_task_branch_deleted` (best-effort; if branch already gone, treat as success)
  5. `POST /api/tasks/<id>/status` with body `{"status":"complete"}` – checkpoint: `task_marked_complete`
  6. `POST /api/tasks/<id>/archive` (no body) – checkpoint: `task_archived` (best-effort; argus archives the task and cleans up the worktree)
- Returns: `{Checkpoints: []string, Error: string}` (Error empty on full success)
- Scenarios: happy full path, mid-step failure returns checkpoints reached, re-invocation after partial success skips already-done checkpoints (idempotent), already-completed task returns immediately
- Argus endpoints (verified to exist in `/Users/aaron/Development/Personal/argus/internal/api/routes.go`):
  - `POST /api/tasks/{id}/status` (body: `{"status":"complete"}`)
  - `POST /api/tasks/{id}/archive` (no body)
- Add these to `internal/argus/tasks.go` as `SetTaskStatus(ctx, taskID, status)` and `ArchiveTask(ctx, taskID)` client methods.

## Pre-resolved decisions (uncomfortable bets)

These are choices I made so the build can proceed autonomously. Aaron should review them in the morning – they're not blockers, but they're places where the spec could land differently with more discussion.

1. **`complete_task` failure contract: checkpoint per sub-step, return reached checkpoints in result.** Caller re-invokes to resume. Each sub-step is idempotent (e.g., "already merged" check before re-merging, branch-already-deleted treated as success).

2. **`run_build` streaming: deferred to a follow-up.** v1 returns combined output on completion. Streaming requires argus SSE plumbing that doesn't exist yet on the iris side. Documented in the delta and design.md Risks/Trade-offs.

3. **Per-repo build lock: per-WORKTREE.** Distinct from the per-source-repo git mutex. Two builds in two worktrees run concurrently; two builds in the same worktree serialize.

4. **`gh_pr_merge` CI gate: caller is responsible.** No automatic gh status check before `gh pr merge`. The spec documents this. Adding a CI gate later is non-breaking.

5. **`complete_task` step ordering: merge → push → delete-remote-branch → mark-complete → archive.** Deleting the remote task branch BEFORE marking the task complete (so a failed argus-side status update doesn't leave a dangling branch). Archive last because it deletes the worktree iris is running git ops against.

6. **`iris:push` only pushes the task branch.** Does NOT push tags or other branches. Verb stays narrow; tag pushing is a future verb if needed.

7. **`iris:gh_pr_create`: title is required; body is optional and defaults to empty.** No template substitution from commit messages – the caller (agent) is responsible for crafting the body.

## Standing authorizations

- **/doitright is the default disposition** for any ralph-review or spec-audit QUESTION item where there's a clear long-term-correct option. Surface "uncomfortable bets" in commit messages and append entries to the bet log at the bottom of this file.

- **Can do:** edit files, commit, push to `argus/handoff-bootstrap-iris-plugin`, create per-verb branches (the Agent tool worktrees handle this), merge agent branches into this one, update PR #1's description, run `make test`, `go vet`, `openspec validate`.

- **Cannot do without explicit confirmation:** merge PR #1 to main, force-push anywhere, `git reset --hard`, `git branch -D`, delete the worktree, run `rm -rf`, install plugins/MCP servers from non-allowlisted sources, send Slack/email/notifications.

## Escalation criteria (when to park a question for Aaron)

Park as a question in `QUESTIONS-FOR-AARON.md` (create if missing, append if exists) when ANY of:

- An agent reports being stuck on something the existing patterns don't cover.
- ralph-review surfaces a finding where neither option is clearly the LTC pick.
- spec-audit finds a gap in a capability whose ownership is ambiguous.
- Two reasonable interpretations of `SKETCH.md` give materially different code.
- An argus endpoint behaves differently from what's documented in this file (e.g., 404 on `POST /api/tasks/<id>/status`).

Format each question per Aaron's interaction preferences (see `~/.claude/CLAUDE.md` "Asking for a decision" section): decision, options, tradeoffs, recommendation, /doitright long-term call-out.

## Exit criteria

ALL must be true for the work to be "done":

1. All 5 verbs implemented + tested + registered
2. `make test` passes under `-race`
3. `go vet ./...` clean
4. `openspec validate --all --strict` clean
5. `bash -n setup.sh` clean
6. ralph-review run; auto-fixes applied; questions parked if any
7. spec-audit run; findings documented; auto-fixes applied
8. PR #1 description updated to reflect assembled scope
9. All commits pushed to `origin/argus/handoff-bootstrap-iris-plugin`
10. `NEXT.md` replaced by `STATUS.md` showing morning-review state

## Starting commands (for the fresh session)

```bash
# 1. Verify state
git -C /Users/aaron/.argus/worktrees/Iris/handoff-bootstrap-iris-plugin status
git -C /Users/aaron/.argus/worktrees/Iris/handoff-bootstrap-iris-plugin log --oneline -6
make -C /Users/aaron/.argus/worktrees/Iris/handoff-bootstrap-iris-plugin test
```

Then read these files in order:

- `NEXT.md` (this file)
- `SKETCH.md` (full design context)
- `openspec/changes/bootstrap-iris-plugin/design.md` (architectural decisions already made)
- `openspec/changes/bootstrap-iris-plugin/specs/iris-host-bridge/spec.md` (the shared-plumbing contract)
- `internal/verbs/merge_to_master.go` (the verb pattern to mirror)
- `internal/verbs/merge_to_master_test.go` (the test pattern to mirror)

Then spawn 4 parallel agents in one message (one per phase-1 verb).

## Known argus endpoints (verified)

REST:

- `GET /api/tasks/{id}` – task record incl. `worktree_path`
- `POST /api/tasks/{id}/status` – set status; body `{"status":"complete"}`
- `POST /api/tasks/{id}/archive` – archive task (cleans up worktree per server-side logic)
- `GET /api/projects/full` – project list with `name` + `path`
- `POST /api/mcp/tools` – register MCP tool
- `DELETE /api/mcp/tools/{name}` – unregister

Daemon socket at `~/.argus/daemon.sock`:

- `Daemon.Ports` – discover REST port
- `Daemon.Ping` – liveness probe

## Reference

- Sibling plugin (similar architecture, more mature): `/Users/aaron/Development/Personal/hera/`
- Test conventions: `internal/argus/recovery_test.go` and `internal/verbs/merge_to_master_test.go` are the references
- Sketch / north-star: `SKETCH.md` at repo root

## Bet log (uncomfortable calls made autonomously – Aaron's morning reading)

Format: `YYYY-MM-DD: <decision> – <one-line reason>. Reversible if Aaron disagrees.`

- 2026-05-25: Loop 3 reverted source-repo path stripping from MCP responses. Aaron pre-confirmed the strip was security theater.
- 2026-05-26 (pre-clear): Decision points 1–7 above are my best calls. Each is reversible.
- (append below as more get made):
