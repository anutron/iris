## Why

Every merge verb iris exposes today assumes a two-branch world: a task branch merges into *the* default branch (`iris:merge_to_master`), or a task branch is cherry-picked onto some other branch (`iris:cherry_pick`, commit-at-a-time). Neither fits a long-lived, non-default **integration branch** (e.g. `integration/big-feature`, a release-staging branch) that repeatedly absorbs whole branches via real merges over its lifetime. Today the only way to do that through iris is to hand-drive `git` directly against the source repo's live checkout — exactly the host-side git shelling iris exists to replace.

Separately, `iris:gh_pr_create` already lets an agent pick which **repo** a PR targets (`base_repo`) but not which **branch** within that repo it targets — `--base` is always the resolved default branch (or omitted so gh picks one). An agent opening a PR into an integration branch (or any non-default target branch) cannot do it through iris today.

## What Changes

- **New verb `iris:merge_to_branch(task_id, target_branch, source_ref, no_ff, message, dry_run)`**: merges (never cherry-picks) `source_ref` into `target_branch` and pushes, using a scratch `git worktree add` in a temp directory so the source repo's current checkout is never touched. `target_branch` and `source_ref` are both caller-supplied and may be arbitrary (any branch, tag, or SHA) — the verb is not scoped to `argus/`-prefixed branches the way `iris:merge_to_master` is. New guard logic (not the existing `guardBranch`) refuses empty/dash-leading args, a branch merged into itself, and targeting the default/protected branch (that's what `iris:merge_to_master` is for). Supports `dry_run` (preview via `--no-commit --no-ff`, same shape as `iris:merge_to_master`'s dry run) and a `.iris.toml` `[post_merge]` hook, read from the merged target branch's tree (not the source repo's untouched checkout).
- **Modified `iris:gh_pr_create`**: add optional `base` (target branch, distinct from `base_repo` which targets a repo). When provided, `--base <base>` is passed instead of the default branch / omitted `--base`, and composes with all three existing target modes (`base_repo`, cross-fork, same-repo-on-origin). Same leading-dash flag-smuggling guard as `head`/`base_repo`.

## Capabilities

### New Capabilities

- `iris-merge-to-branch`: merge an arbitrary source_ref into an arbitrary long-lived target branch via a scratch worktree, then push; dry-run preview and post_merge hook support.

### Modified Capabilities

- `iris-gh-pr-create`: adds an optional `base` parameter selecting the target branch, composing with `base_repo` / cross-fork / same-repo modes.

## Impact

- `internal/verbs/merge_to_branch.go` (new) — `MergeToBranch`, scratch-worktree setup/teardown, guard logic, dry-run, post_merge hook runner.
- `internal/verbs/merge_to_branch_test.go` (new).
- `internal/mcp/handler_merge_to_branch.go` (new).
- `cmd/iris/merge_to_branch.go` (new) — `iris merge-to-branch` CLI.
- `cmd/iris/main.go` — register the new subcommand.
- `internal/daemon/run.go` — register the `iris_merge_to_branch` handler (L72-96) and its tool schema in `toolDefinitions()` (L190+).
- `internal/verbs/gh_pr_create.go` — add `Base` to `GHPRCreateOptions`; compose into all three target-mode branches.
- `internal/verbs/gh_pr_create_test.go` — new `base` composition tests.
- `internal/mcp/handler_gh_pr_create.go`, `cmd/iris/gh_pr_create.go` — accept/pass `base`.
- `internal/daemon/run.go` — `iris_gh_pr_create` schema gains `base`.
- `openspec/specs/iris-merge-to-branch/spec.md` (new via archive), `openspec/specs/iris-gh-pr-create/spec.md` (delta via archive).
- `README.md` — CLI table + verb docs.
- No data, migration, or cross-service impact.
