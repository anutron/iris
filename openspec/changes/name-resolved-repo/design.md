## Context

Every task-scoped verb calls `verbs.Resolve` (or `ResolveSelf` / `ResolvePath` for self-hosting verbs), and the MCP server's `handleCallback` is the single path every MCP response takes. About half of the verbs already return a `source_repo` path in their own result shape. The rest, including every `gh_pr_*` verb, return nothing that identifies the repo.

## Goals / Non-Goals

**Goals:**

- Every MCP response, success or error, names the repo iris actually resolved, with no per-verb code.
- Make the irreversible PR verbs that take a caller-supplied identifier (merge, close) preventable, not only detectable.

**Non-Goals:**

- No `expected_repo` on push, tag, `merge_to_master` or `complete_task`. They act on the task's own branch or the default branch, so a wrong `task_id` does not hit a lookalike target, and the `repo` field is enough.
- No `repo` field on CLI output. A human at the host shell runs the CLI in a known repo. The CLI keeps calling the same verb functions, so its argument contract is unchanged.
- No fix for `gh_pr_merge`'s unlocked post-merge checkout (noted separately in `.workflow/todo/`).

## Decisions

### A request-scoped recorder, annotated by the server

`handleCallback` installs a `verbs.RepoRecorder` in the request context. `Resolve`, `ResolveSelf` and `ResolvePath` record their result into it, and the first record wins. After the handler returns, the server injects `"repo": {"name", "path"}` as the first key of a JSON-object success body, or appends `repo: owner/name (path)` as a final line to an error or non-JSON body. Verbs that never resolve (`iris_ls`) are untouched.

**Alternatives:** adding a `Repo` field to ~25 result structs touches every verb and is easy to forget on the next one. Re-resolving the `task_id` in a wrapper doubles the argus round trips and can't see what the verb actually resolved (for example `ResolveTarget`'s path branch).

### `name` comes from the origin remote URL, with no network call

`git remote get-url origin` is parsed for GitHub-style `scp`, `ssh://` and `https://` forms into `owner/name`. If it can't be parsed (a local-path remote, no origin), `name` is omitted and `path` is always present. The lookup only runs when a recorder is present, so the CLI and the direct-call tests pay nothing.

### PR identity comes from gh itself

`gh pr merge` resolves its repo with gh's own rules, which can pick an `upstream` remote or a `gh repo set-default` target over origin. So the merge and close verbs run `gh pr view <n> --json url,title` first and report `pr_url` and `pr_title`. The guard compares `expected_repo` with the `owner/name` parsed from that URL, case-insensitively. That is the repo gh will act on, not the one iris guesses.

The pre-flight is best-effort without a guard: if it fails, the verb proceeds as before and adds a warning (merge) or leaves the fields empty (close). With `expected_repo` set, it fails closed: if the PR can't be identified, nothing is merged or closed.

`expected_repo` must look like `owner/name`. Anything else is rejected before gh is invoked.

## Risks / Trade-offs

- **One extra gh API call per merge or close** (~0.5 s). This is acceptable for an irreversible action.
- **The `repo` key could collide with a verb's own top-level `repo` field.** None exists today, and the server skips injection if one appears.
- **Key order:** injection splices into the indented JSON text instead of re-marshalling, so existing field order is preserved.
