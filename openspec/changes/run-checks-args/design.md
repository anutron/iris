## Context

Sandboxed Sketch agents run rspec and jest directly and leak hung processes for days. Sketch will route all sandbox test runs, full suites and single files, through `iris_run_checks`. `RunChecks` today takes one `check` token and `runWorktreeCommand` uses `exec.CommandContext`, which on the ceiling kills only the direct child. `script/iris-check` is typically a shell script that launches rspec or jest, which spawn workers, so the grandchildren survive with PPID 1. `WaitDelay` already stops them holding the run open, but nothing reaps them.

The repo already has the process-group pattern twice: `internal/secrets/resolver.go` and `internal/verbs/reload.go` set `Setpgid` and a `cmd.Cancel` that does `syscall.Kill(-pid, SIGKILL)`.

## Goals / Non-Goals

**Goals:**

- Let a caller name test paths per run without any shell or token encoding.
- Make `args` safe to accept from a sandboxed agent: no option injection, no shell, bounded size.
- Make the ceiling reap everything the script left in its process group, for `run_checks` and `run_build`.

**Non-Goals:**

- Validating that args are existing paths or stay inside the worktree. The script owns its argument semantics, and arbitrary agent-chosen files in its own worktree are not a privilege boundary here.
- Reaping descendants that deliberately leave the process group (`setsid`, daemonizing). Only group members are covered.
- Killing the group when the script exits normally. Only the ceiling kills; after a normal exit stragglers are left, as today, and `WaitDelay` still frees the run.
- Cancelling a run on demand.
- A per-repo configurable limit or ceiling.

## Decisions

### `args` is a separate list appended to argv

`args: []string` is passed as `exec.Command(script, append([]string{check}, args...)...)`. No shell runs anywhere, so quoting is moot. Encoding paths into `check` (`test:spec/a.rb`) was rejected: it makes the check token a mini-language that each repo's script must parse, and it blurs the validated `check` name with agent-chosen data.

### Reject, don't sanitize

Any arg starting with `-` is rejected. The script hands args to rspec or jest, where a leading dash is an option (`--require evil.rb`, `--config`, `--runInBand` are benign but `--require` or `--setupFiles` run arbitrary code outside the intended paths). Rejecting leading dashes makes every arg a positional. The cost is that a path literally starting with `-` can't be passed, so the caller can use `./-weird.rb`. An end-of-options `--` was rejected as the fix: iris can't know whether the repo's script forwards it. NUL is rejected because it can't appear in an argv entry and, in the registry key, would break separator uniqueness. Empty strings are rejected because a blank positional makes rspec and jest treat it as the whole suite or error, and is never a useful path. Failing loudly beats guessing.

### Limits: 64 args, 512 bytes each

A real single-file or few-file run passes one to a handful of paths; 64 leaves room for a batch such as a CI shard without letting an agent push a whole file listing. Typical repo-relative test paths are well under 200 bytes, so 512 leaves ample headroom while staying below `PATH_MAX` (1024 on macOS) so it doesn't permit pathological values. Worst case is 64 x 512 = 32 KiB, far below the `ARG_MAX` of 1 MiB on macOS and the handle and registry key sizes. Both limits are constants in `verbs`, checked inclusively, and can be raised later without breaking callers.

### Validation lives in the verb

`RunChecks` validates `Args` first, before resolving the task, so both MCP and CLI get the same errors and nothing is registered or locked on bad input. The MCP handler also calls the same validator before `runs.Start`, so a bad call fails immediately instead of returning a handle that later fails.

### `args` join the duplicate-run key

`runs.Start` builds the key as `tool NUL task_id NUL arg`. For `run_checks` the `arg` becomes `check NUL arg1 NUL arg2 ...`. Because NUL is rejected in `check`-adjacent args, the encoding is injective: `check="a"` with `args=["b"]` can't collide with `check="a b"`. Order is significant, so `[a, b]` and `[b, a]` are different runs. The key is internal; the handle's fields are unchanged.

### Process group with a SIGKILL cancel

`runWorktreeCommand` sets `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` and `cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }`, mirroring `secrets/resolver.go` and `reload.go`. `exec.CommandContext` invokes `Cancel` only when the ceiling context ends. The caller-detached run is unaffected by caller cancellation, since `context.WithoutCancel` is already applied. `ESRCH` from an already-exited group is ignored. `WaitDelay` stays at 1 second to free the pipes. `reload.go` and `secrets/resolver.go` already use `syscall.Kill` and `Setpgid` unguarded, so no new platform constraint arises. Alternatives considered: walking the process tree with `pgrep -P` (racy, slow) and `Pdeathsig` (linux only, parent-death not ceiling).

### Testing the kill

The test uses the existing `worktreeRunTimeout` var, shrunk to a fraction of a second, with a fixture script that backgrounds `sleep` writing its PID to a file in a nested subshell, then waits. After the run returns `[timeout]` the test polls `syscall.Kill(pid, 0)` until `ESRCH`, failing after a few seconds.

## Risks / Trade-offs

- **A script that `setsid`s its workers escapes the group** → Accepted and documented; the ceiling still fails the run and `WaitDelay` frees the lock. Normal rspec and jest launches stay in the group.
- **Process group is a new kill radius** → The group contains only the script and what it spawned, because `Setpgid` creates a fresh group with the script as leader, never the iris daemon's own group.
- **Leading-dash rejection blocks legitimate `-` paths** → Pass them as `./-name`. Failing loudly is preferred over a script reinterpreting an option.
- **Different args queue serially behind the worktree lock** → Same as any two runs in a worktree today; the 30-minute ceiling covers the wait.
- **The repo's script may misuse args** → Out of scope; iris guarantees well-formed argv only, and the SKILL docs say the script must treat extra args as paths.

## Migration Plan

Additive. `args` is optional, so existing callers and scripts see no change. The process-group kill only changes behaviour at the ceiling. Ships with the next daemon reload; rollback is a revert.

## Open Questions

None.
