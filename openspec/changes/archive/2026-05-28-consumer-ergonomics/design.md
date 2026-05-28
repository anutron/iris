## Context

Iris already integrates with argus (the argus client is part of iris's verbs). The architectural question this change resolves: when do we put a recipe in iris vs. expose primitives the consumer chains? Three of the six feedback items push iris to bake in argus-specific behavior (auto-detecting task_id from path, running `go install + kill -TERM + iris_complete_task` after every merge, generating "do this next" hints). The decision here is to:

1. **Echo, don't recipe** – iris exposes resolved task data, factual postconditions, and a generic per-repo hook. It does NOT prescribe the consumer's next action.
2. **Keep argus coupling at the data layer only** – `argus.Client.ListTasks()` is fine because the argus client already exists in iris. Adding it doesn't extend iris's consumer-awareness; it makes the existing dependency more useful.
3. **Push consumer-specific recipes into config** – `.iris.toml`'s `[post_merge]` block lets each repo declare its own tail (argus repos can run `go install ./...; pkill -TERM -F ~/.argus/daemon.pid` etc.; non-argus repos do whatever fits). The recipe lives next to the code it serves, not in iris.

## Decisions

### Decision 1: `argus_task` field on StatusResult instead of generic `consumer_metadata`

The original design suggested a generic `consumer_metadata` blob populated at worktree-create time. After looking at the code, iris already imports `argus.Task` and uses it pervasively. Echoing the resolved Task as `argus_task` on the status result is more honest about the existing coupling and is what consumers actually want. A `consumer_metadata` indirection would just be an `argus.Task` rename today.

The trade-off: if a future non-argus consumer ever uses iris, this field is dead weight (left as null). That's acceptable. Iris is built for argus; opening it up to other consumers is a separate change with its own design space.

### Decision 2: `ListTasks` not `FindTaskByWorktreePath`

Argus has a list endpoint; iris filters client-side. Reasons:

- Argus already supports the list endpoint per existing handlers iris consumes.
- Filtering by worktree path is iris's concern – the matching algorithm (`EqualSourceRepos` with canonicalization) belongs in iris.
- Keeps the argus client thin: one new method, returns all tasks, no server-side query language.

If the task list grows large enough to make this expensive (>>100 tasks), we can introduce a server-side filter later. Today the list is small.

### Decision 3: `dry_run` reuses the real merge path with `--no-commit`

A fake dry-run that only lists files but misses conflicts is worse than nothing. Real implementations are either:

- **`git merge --no-commit --no-ff` then `--abort`** – attempts the actual merge, captures the full state, aborts cleanly. This is what we'll do.
- **Three-way merge into a temp index** – more complex, no benefit over `--no-commit`.

The dry-run path holds the same per-source-repo lock as a real merge (so a real merge can't sneak in between), runs `fetch + checkout default + pull --ff-only` as usual, then `merge --no-commit --no-ff`. Whether the merge succeeds or hits conflicts, we always `merge --abort` before returning. The result includes `would_succeed: bool`, `files_changed: [...]`, and `conflicts: [...]`. The `post_merge` hook does NOT run.

### Decision 4: `[post_merge]` is shaped like `[pre_flight]` / `[verify]`

`.iris.toml` already has the `HookBlock` shape for `[pre_flight]` and `[verify]`. `[post_merge]` reuses it directly – same `command`, `working_directory`, `timeout_seconds` fields, same validation. The only new thing is the execution point and the exported env vars.

Env exported to the hook:

- `IRIS_TASK_ID` – the argus task ID
- `IRIS_TASK_BRANCH` – the branch that was merged (e.g. `argus/feature-x`)
- `IRIS_SOURCE_REPO` – absolute path to the source repo
- `IRIS_DEFAULT_BRANCH` – `main` / `master`
- `IRIS_MERGE_SHA` – the new HEAD on the default branch after merge

If the hook exits non-zero, iris reports the failure in the result but does NOT roll back the merge. The merge succeeded; the post-merge step is informational. Consumers can examine `post_merge.exit_code` to decide their own behavior.

### Decision 5: Missing-config is silent; parse errors are loud

Today, `LoadIrisToml` synthesizes a `ValidationError` for ENOENT, which `iris:status` surfaces as a warning. That's overloaded – a parse error and a missing file are not the same severity. After this change:

- File missing (`fs.ErrNotExist`): `LoadIrisToml` returns `(nil, nil, nil)`. Callers that need a config explicitly check `doc == nil` and synthesize their own error.
- File present but malformed: unchanged – ValidationErrors flow as before.

`iris:status` reports `config: null` with no warning when missing. `iris:merge_to_master` doesn't require `.iris.toml` to function (post_merge is optional), so the new silent-missing contract is fine. Verbs that DO require a config (e.g., `iris:reload`) keep their existing error path because they already check `doc == nil` explicitly.

### Decision 6: Postconditions are facts, not directives

The recommendation was to NOT emit "next: call iris_complete_task" hints because that prescribes the consumer's workflow. Instead, expose factual postconditions iris can attest to:

- `task_branch_still_exists: bool` – self-explanatory; after a successful merge, this is `true` (we don't delete the branch).
- `worktree_still_present: bool` – iris doesn't delete the worktree either, so `true` post-merge.

Both are always `true` today, but the consumer can read them as documentation: "merge does not clean up." When `dry_run: true`, the same fields are reported about state-before-the-attempt, which is also always `true`.

The fact that the values are constants today is fine. The field NAMES are the documentation. If a future merge variant elects to delete the branch (e.g. `iris:merge_to_master` with a `delete_branch: true` option), the field flips meaningfully without a schema change.

## Risks / Trade-offs

- **`ListTasks` perf** – proportional to the argus task count. Fine today. Mitigation: filter server-side later if it becomes hot.
- **`post_merge` hook security** – arbitrary command execution from `.iris.toml`. This matches the existing `[pre_flight]` / `[verify]` / `[build]` blocks; same trust model (`.iris.toml` is part of the source repo, only the operator can edit it).
- **Dry-run state leak** – if the `--no-commit` merge succeeds but the abort fails, the source repo is left mid-merge. Same risk as the real merge path's existing `merge --abort` cleanup. The deferred-cleanup pattern from the existing `mergeToMasterLocked` covers this.
- **Backwards compat** – all new fields on response objects are additive. Existing consumers ignore them. The `dry_run` input defaults to `false`, matching prior behavior. The silent-missing-config change is observable to status callers but the warning was noise, not contract; no consumer should depend on its presence.

## Migration Plan

None. All changes are additive or noise-reduction. No `.iris.toml` migrations required (the file is optional and the new field is optional within it). No consumer code changes are required to continue working; consumers opt into the new fields by reading them.
