---
name: iris
description: >-
  Use ONLY inside an argus sandbox (cwd under ~/.argus/worktrees/ or
  ARGUS_TASK_ID set), where iris registers `mcp__argus__iris_*` tools. Reach for
  this when you need to act on the canonical source repo on the HOST — push a
  branch, open/merge/comment on a GitHub PR, merge to the default branch,
  cherry-pick, tag, or run the project build — i.e. anything `Bash(git push)` or
  `Bash(gh ...)` would do but can't from inside the sandbox. Not in an argus
  sandbox? The iris MCP tools are not registered; do not use this skill.
---

# iris — host-side git/GitHub bridge for argus sandboxes

## 1. Gate: are you actually in an argus sandbox?

Before anything else, confirm ONE of these holds:

- your current working directory is under `~/.argus/worktrees/` (equivalently `$HOME/.argus/worktrees/`), **or**
- the `ARGUS_TASK_ID` environment variable is set.

If **neither** holds, stop: **the `mcp__argus__iris_*` tools are not registered in this session.** This skill does not apply. If you genuinely need iris's behavior outside a sandbox, call the `iris` CLI binary directly (e.g. `iris merge-to-master <task-id>`, `iris status`) — it runs the same Go functions the MCP handlers do. Do not try to call `mcp__argus__iris_*` tools; they will not exist.

If the gate holds, continue.

## 2. What iris is

Iris is an argus plugin daemon running on the **host**. It performs a fixed, allowlisted set of git and GitHub operations on the canonical source repo on behalf of agents trapped inside argus worktrees. You call an MCP tool like `mcp__argus__iris_merge_to_master(task_id=...)`; iris resolves your `task_id` to the source repo on the host, performs the operation there under a per-source-repo lock, and returns a structured result. It is **not** a general shell — there is no command-passthrough verb, ever.

## 3. Core mental model — why you need iris at all

Your worktree is a real git checkout, and **local git inside it is fine**: `git status`, `git diff`, `git log`, `git add`, `git commit`, `git branch`, creating/switching your own feature branch — all of that works in the sandbox and you should just use `Bash` for it.

What you **cannot** reach from inside the sandbox is everything that lives on the host *outside* your worktree:

- the **canonical source repo** (the real clone the worktree was spun off from),
- the **`origin` remote** (pushing needs host credentials + network the sandbox doesn't have),
- **GitHub** (the host's `gh` auth).

Iris is the bridge to exactly those. Every iris verb that touches the source repo resolves it from your `task_id` — you never pass a filesystem path, and iris refuses paths outside argus's project allowlist. Branch-moving verbs refuse anything not prefixed `argus/` and refuse to clobber the default branch.

## 4. Tool surface

All tools are registered as `mcp__argus__<name>`. Names below omit the prefix. Unless noted, every verb takes `task_id` (required) and uses it to resolve the source repo.

### Ship (get your branch into the default branch / origin)

- **`iris_push`** — push the task's `argus/*` branch to `origin` (force-with-lease). Use whenever you'd reach for `git push`. Refuses the default branch. Pass `remote` to push to a different **configured** remote (a name, never a URL) — e.g. `remote="upstream"` to push the branch to an upstream you have write access to so its CI runs (see the cross-fork note below). Runs under iris's own timeout (`git_transfer_timeout_seconds` in `.iris.toml`, default 300s), not your request's — a large or far-diverged push isn't killed just because you stopped waiting. A failure names its kind: `[timeout]` means iris's own deadline fired (check `iris_fetch`/`iris_status` before assuming success or failure — don't blindly retry); `[auth_failure]`/`[network_failure]` mean fix credentials/connectivity before retrying; `[other_failure]` is everything else (e.g. non-fast-forward).
- **`iris_merge_to_master`** — merge the task's branch into the source repo's default branch (master/main) under lock. Does **not** delete the branch or worktree. Pass `dry_run: true` to preview (`would_succeed`, `files_changed`, `conflicts`) without committing.
- **`iris_complete_task`** — the composite ship-it: merge → push default branch → delete remote task branch → mark the argus task complete → archive. Each sub-step is a checkpoint; a partial failure returns checkpoints reached so a retry resumes. Use this when you're done and want one call to finish everything.

### PR lifecycle (host `gh` CLI)

- **`iris_gh_pr_create`** — open a PR for the task's branch (`title` required; `body`, `draft` optional). Refuses to open from the default branch. Returns PR number + URL. Target selection: pass `base_repo="owner/repo"` to open a **same-repo PR on that repo** (skips fork detection; the branch must already exist there — push it first with `iris_push remote=...`); otherwise, when `origin` is a fork, it opens a **cross-fork** PR into the upstream parent automatically (`--repo <upstream> --head <fork-owner>:<branch>`); otherwise a same-repo PR on `origin`. **CI caveat:** GitHub does not run CI on cross-fork PRs from a fork — if you need CI, push to the upstream (`iris_push remote=...`) and use `base_repo` so the PR is same-repo there.
- **`iris_gh_pr_view`** — read a PR's state (`pr_number`); returns `gh`'s JSON (state, checks, reviews, mergeable, isDraft, statusCheckRollup, …). This is your polling tool for "is this PR green / ready to merge?".
- **`iris_gh_pr_ready`** — take a draft PR out of draft (idempotent; reports whether it changed state).
- **`iris_gh_pr_comment`** — post a comment to a PR (`body`). Returns the comment URL.
- **`iris_gh_pr_merge`** — merge a PR (`pr_number`, `strategy` = squash|merge|rebase). v1 does not pre-check CI; `gh` surfaces a failure if required checks are red — poll `iris_gh_pr_view` first.
- **`iris_gh_pr_close`** — close a PR without merging (optional `delete_branch`).

### Branch & history (on the source repo)

- **`iris_fetch`** — `git fetch origin` in the source repo; returns refs whose tracking SHAs changed. Use before deciding whether you're behind. Same timeout/classification behavior as `iris_push` (above).
- **`iris_branch_create`** — create a branch in the source repo from an arbitrary `base_ref` (e.g. `origin/master`). Does **not** change the source repo's checkout. Pair with `iris_checkout` to switch.
- **`iris_checkout`** — switch the source repo to a branch. `force=false` (default) propagates git's refusal on a dirty tree; `force=true` aborts any in-progress merge/cherry-pick/rebase and discards changes first — the recovery path for a stuck source repo.
- **`iris_cherry_pick`** — checkout `target_branch` then cherry-pick `commit` under lock; aborts cleanly on conflict (returns conflict paths, leaves a clean tree). Refuses the default branch as target — use `iris_merge_to_master` for that.
- **`iris_branch_delete_remote`** — delete a remote branch via `git push origin :<branch>`. Refuses the default branch. Returns the prior remote SHA.
- **`iris_tag`** — create an annotated tag at `origin/<default-branch>` and push it (`tag` name, optional `message`). Refuses if the tag already exists.

### Build & checks

- **`iris_run_build`** — run the project's build in the worktree (`script/iris-build` if present, else the Makefile `build` target). Returns command, exit code, and combined output; non-zero exit still carries the output so you see compile errors. Use to gate a PR/merge on a clean build.
- **`iris_run_checks`** — run a repo-defined quality check in the worktree via `script/iris-check <check>` (e.g. `check="lint"`, `"test"`, `"security"`). Script-only, no Makefile fallback; errors naming `script/iris-check` if it's absent or non-executable. Returns command, exit code, and combined output; non-zero exit still carries the full output, so you read the real rubocop/rspec/brakeman text host-side instead of waiting on CI. `check` is a single token passed as an argv element to the repo-controlled script, not a shell string.

### Self-management (rarely what a task agent wants)

These manage iris-style daemons via a repo's `.iris.toml` (and the optional per-developer `.iris.local.toml` overlay). A normal task agent shipping code almost never needs them; they're for operating iris (or another iris-managed daemon) itself.

- **`iris_status`** — for one managed system: resolved config, current HEAD/branch, default branch, origin SHA, clean-tree state, matching argus task, and last reload/publish outcome. `task_id` and `path` are mutually exclusive; omit both to target iris itself.
- **`iris_ls`** — list systems iris has reloaded/published recently (from the audit log).
- **`iris_validate_config`** — parse + cross-validate the resolved `.iris.toml` (+ `.iris.local.toml` overlay) with no side effects. Your edit→validate→fix loop when **authoring a config**.
- **`iris_set_local_config`** — write/merge `.iris.local.toml` (per-developer fields only, e.g. `dogfood_branch`). Refuses shared fields, validates per-field, writes atomically. Prefer this over hand-editing the local file.
- **`iris_reload`** — live-upgrade an iris-managed daemon (pull default branch, build, restart per `.iris.toml`). Omit `task_id`/`path` to reload iris itself.
- **`iris_publish`** — from a worktree, update the source repo's *current* branch to your HEAD, then rebuild+restart via `.iris.toml`. `reset=true` for hard reset; `push=true` also pushes (non-default branch). For the build-deploy-this-checkout loop, not for shipping a PR.

### Authoring `.iris.toml` / `.iris.local.toml`

When you need to **create, edit, or debug an iris config** (onboarding a repo to iris-managed reload, adding a dogfood/ship setup, or fixing a config `iris_validate_config`/`iris_reload` rejects), read **`config-authoring.md`** in this skill directory. It is the full schema reference: every field and which of the two files it belongs in, the `[build]`/`[restart]`/hook blocks, the six restart mechanisms and the `exit_code`-is-self-only rule, the overlay/merge semantics, the validate→fix authoring loop, and worked example configs. The 30-second version:

- `.iris.toml` (checked in, project-wide) declares **how to build + restart** the daemon: `schema_version = 1`, optional `default_branch`, a required `[build] command = [...]` (argv, no shell), and a required `[restart] mechanism = "..."`.
- `.iris.local.toml` (gitignored, per-developer) overlays **machine-specific** fields only — `dogfood_branch`, `ship_ci_timeout_seconds`. Write it via `iris_set_local_config`, not by hand.
- Validate every edit with `iris_validate_config`; it has **no side effects** and returns per-field errors with remediation hints.

## 5. When to use what

- **Done with the task, want it merged and cleaned up in one shot** → `iris_complete_task`.
- **Want a PR to live for review (not auto-merge)** → `iris_push` then `iris_gh_pr_create`. Mark ready later with `iris_gh_pr_ready`.
- **Merge straight to the default branch, no PR** → `iris_merge_to_master` (preview first with `dry_run: true` if unsure it's clean).
- **"Is the PR ready?"** → poll `iris_gh_pr_view`; merge with `iris_gh_pr_merge` only once checks are green.
- **Land a fix on a release/other branch** → `iris_branch_create` + `iris_cherry_pick` (or `iris_checkout` first), then `iris_push`.
- **Source repo is wedged mid-operation** → `iris_checkout(force=true)`.
- **Just inspecting local state** → plain `Bash` git in your worktree. Don't call iris for `git status`/`git diff`/`git log`.

## 6. Common Bash mistakes (and the iris tool to use instead)

These `Bash` calls will fail or do the wrong thing inside the sandbox because they target host state outside your worktree:

| You're about to run… | Why it fails here | Use instead |
| --- | --- | --- |
| `git push` / `git push -u origin …` | no host creds / network to `origin` | `iris_push` |
| `gh pr create` / `gh pr merge` / `gh pr view` / `gh pr comment` | host `gh` auth lives outside the sandbox | `iris_gh_pr_create` / `iris_gh_pr_merge` / `iris_gh_pr_view` / `iris_gh_pr_comment` |
| `git checkout master && git merge argus/…` | the default branch + canonical repo aren't your worktree's to move | `iris_merge_to_master` |
| `git tag vX && git push --tags` | tagging+push targets `origin` | `iris_tag` |
| `git push origin :old-branch` | remote-branch delete needs `origin` | `iris_branch_delete_remote` |
| editing the *source repo's* checkout directly | it's not mounted in your sandbox | `iris_checkout` / `iris_branch_create` / `iris_cherry_pick` |

What is **fine** as plain `Bash` in your worktree: `git status`, `git diff`, `git log`, `git add`, `git commit`, `git branch`, `git switch -c argus/...`, building/running tests locally.

## 7. Composition with sibling plugins

- **hera** (`mcp__argus__hera_*`) — multi-agent orchestration. An orchestrator fans work out to worker sessions, each in its own worktree. The seam: a worker does the coding and local commits, then uses **iris** to push / open a PR / merge its branch back to the canonical repo. hera coordinates *who does what*; iris performs the *host-side landing*.
- **plannotator-argus** (`mcp__argus__plannotator_*`) — the review UI. Use it to get a PR or working-tree reviewed (`plannotator_review`) before you ship. Typical order: finish work → `plannotator_review` → address feedback → `iris_push` / `iris_gh_pr_create`.
- **argus core** (`mcp__argus__task_*`) — task lifecycle. `task_complete` marks an argus task done but does **not** touch git. `iris_complete_task` is the superset that also merges, pushes, deletes the remote branch, and archives. If you only need git landing without argus bookkeeping, use the individual iris ship verbs; if you want the whole thing finished, use `iris_complete_task` (it calls into argus to mark complete for you).

## 8. Worked workflows

### A. Ship a finished task (PR-gated merge)

```
iris_run_build(task_id)                       # confirm it compiles on the host
iris_push(task_id)                            # push argus/<task> to origin
iris_gh_pr_create(task_id, title="…")         # open the PR -> {number, url}
# poll until checks are green:
iris_gh_pr_view(task_id, pr_number=N)         # inspect statusCheckRollup / mergeable
iris_gh_pr_merge(task_id, pr_number=N, strategy="squash")
iris_branch_delete_remote(task_id, branch="argus/<task>")   # tidy origin
```

Or collapse the merge+push+delete+complete+archive tail into one call once the PR is green (or if you're merging without a PR):

```
iris_complete_task(task_id)
```

### B. Open a PR for review, then mark it ready

```
iris_push(task_id)
iris_gh_pr_create(task_id, title="…", draft=true)   # draft while review happens
# … reviewers comment; you push more commits via iris_push …
iris_gh_pr_ready(task_id, pr_number=N)               # take it out of draft
```

### C. Push to an upstream so CI runs (origin is your fork)

When `origin` is your fork but you have write access to the canonical upstream, a cross-fork PR won't run CI. Push the branch to the upstream and open a same-repo PR there:

```
iris_push(task_id, remote="upstream")                 # branch lands on the upstream (must be a configured remote)
iris_gh_pr_create(task_id, title="…", base_repo="drn/argus")   # same-repo PR on the upstream -> CI runs
```

### D. Cherry-pick a hotfix onto a release branch

```
iris_branch_create(task_id, name="hotfix/x", base_ref="origin/release-1.2")
iris_cherry_pick(task_id, commit="<sha>", target_branch="hotfix/x")
iris_push(task_id)                                   # then open a PR if desired
```

If the source repo is stuck mid-merge from a prior failure, recover first:

```
iris_checkout(task_id, branch="master", force=true)  # abort in-progress op + discard
```

## 9. Gotchas

- **`task_id` is required** for every ship/PR/branch verb — it's how iris finds the source repo. You cannot pass a path. (Only the self-management verbs `status`/`ls`/`validate_config`/`reload`/`publish` may omit it to target iris itself.)
- **Branches must be `argus/`-prefixed** for merge/push, and iris refuses to move or delete the default branch.
- **iris never deletes your worktree or task branch** on merge — that's factual, not a bug. Use `iris_branch_delete_remote` + let argus archive, or `iris_complete_task` for the full cleanup.
- **`iris_gh_pr_merge` doesn't pre-check CI** — poll `iris_gh_pr_view` first.
- Results are structured JSON; on failure you get a structured error (often with captured git/gh output), not a thrown exception — read it.
