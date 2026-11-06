## Why

Sandboxed agents run rspec and jest themselves, and when a run hangs they leak processes for days. The route that fixes this is to send every sandbox test run, whole suites and single files alike, through `iris_run_checks`. Two gaps block that. `iris_run_checks` takes only a check name, so a single-file run has no safe way to name its path. And when a run hits iris's 30-minute ceiling, iris kills only the top-level script, so grandchildren (rspec, jest workers, servers) are orphaned with PPID 1 and keep running.

## What Changes

- `iris_run_checks` (MCP tool and CLI) accepts an optional `args` list of strings, appended after the check name as separate argv entries to `script/iris-check`. No shell is involved, and paths are never encoded into the check token.
- Iris rejects any arg that starts with `-`, contains a NUL byte, or is empty, and caps the list at 64 args of at most 512 bytes each. Rejection happens before anything runs.
- `args` become part of the one-run-per-worktree duplicate key: the same check with different args is a different run, and the same check with the same args still joins the in-progress run.
- When a worktree run hits the 30-minute ceiling, iris SIGKILLs the whole process group of the script, not just the script. This covers `iris_run_checks` and `iris_run_build`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `iris-run-checks`: the verb gains the `args` parameter, with its argv, validation and limits rules.
- `iris-worktree-runs`: the ceiling kills the whole process group for both `run_checks` and `run_build`, and the duplicate-run key includes `args`.

## Impact

- Code: `internal/verbs/worktree_run.go` (process group and cancel), `internal/verbs/run_checks.go` (`RunChecksOptions.Args`, validation, display), `internal/mcp/handler_run_checks.go` and the tool schema in `internal/daemon/run.go`, `cmd/iris/run_checks.go` (CLI flag).
- Docs: `claude/skills/iris/SKILL.md` and `README.md`.
- Compatibility: `args` is optional, so existing callers are unchanged. A repo's `script/iris-check` must treat its extra argv entries as test paths; iris only guarantees they are well-formed.
- `iris_run_build` gains no new parameter, but its ceiling kill now reaches grandchildren.
