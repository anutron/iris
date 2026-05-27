## ADDED Requirements

### Requirement: `iris:publish` verb

The plugin SHALL expose `iris:publish` as both an MCP tool and CLI subcommand accepting `task_id` (string, required), `branch` (string, optional – defaults to the source repo's currently-checked-out branch), `push` (bool, default false), and `reset` (bool, default false). On success the verb SHALL return a structured result naming the target source repo, the branch updated, the worktree HEAD applied, whether a push happened, and the full build + restart output. On failure it SHALL return a structured error and SHALL NOT advance the source repo's ref or working tree past pre-flight.

#### Scenario: Successful ff-only publish updates source repo and rebuilds

- **GIVEN** an argus task whose worktree has a commit ahead of the source repo's currently-checked-out branch, and the worktree's HEAD is a descendant of the source repo's branch tip
- **WHEN** `iris:publish` is invoked with the task_id and no other flags
- **THEN** iris acquires the per-source-repo lock, runs `git -C <source-repo> merge --ff-only <worktree-sha>`, runs the configured build, dispatches the configured restart mechanism, writes an audit entry with `mode = "publish"`, releases the lock, and returns a structured success result

#### Scenario: `--reset` performs a hard reset of ref and working tree

- **GIVEN** a worktree whose HEAD is NOT a descendant of the source repo's branch tip (the histories have diverged)
- **WHEN** `iris:publish` is invoked with `reset = true`
- **THEN** iris runs `git -C <source-repo> reset --hard <worktree-sha>`, then rebuilds and restarts; the source repo's ref AND working tree both move to the worktree's HEAD

#### Scenario: `--push` also pushes the target branch to origin

- **WHEN** `iris:publish` is invoked with `push = true` after a successful local update
- **THEN** iris runs `git -C <source-repo> push origin <branch>` after the local ref update but before the build, and includes the resulting remote SHA in the structured result

### Requirement: Pre-flight refusals before any side effect

`iris:publish` SHALL perform pre-flight checks before acquiring the lock or mutating any state, and SHALL refuse with a structured error if any fails.

#### Scenario: Refuses dirty worktree

- **WHEN** the argus worktree has uncommitted changes (`git status --porcelain` returns non-empty)
- **THEN** iris returns a structured error naming the dirty paths and does NOT acquire the lock or touch the source repo

#### Scenario: Refuses dirty source repo

- **WHEN** the source repo's working tree has uncommitted changes
- **THEN** iris returns a structured error naming the dirty paths and does NOT acquire the lock (avoids clobbering operator's in-progress work via `--reset` or surprising the operator with a half-applied state via `--ff`)

#### Scenario: Refuses missing or invalid .iris.toml

- **WHEN** the source repo root does not contain a readable `.iris.toml`, or the file fails schema validation
- **THEN** iris returns a structured error naming the expected path and validation errors and does NOT acquire the lock

#### Scenario: Refuses source repo outside argus project allowlist

- **WHEN** the resolved source-repo path does not match any allowlisted argus project
- **THEN** iris returns a structured error naming the rejected path and does NOT acquire the lock

#### Scenario: Refuses when target branch is not source repo's current HEAD

- **WHEN** the resolved `branch` (either supplied or defaulted) is NOT the source repo's currently-checked-out branch
- **THEN** iris returns a structured error naming both the requested branch and the source repo's actual current branch, and does NOT mutate the source repo (v1.2 constraint – a future flag may add checkout behavior)

#### Scenario: Refuses non-ancestor worktree HEAD without --reset

- **WHEN** the worktree's HEAD is NOT an ancestor-descendant of the source repo's branch tip and `reset = false`
- **THEN** iris returns a structured error naming both SHAs and pointing at `--reset`, and does NOT advance the ref

### Requirement: Per-source-repo lock spans git update, build, and restart

`iris:publish` SHALL acquire the per-source-repo mutex immediately after pre-flight and SHALL hold it through git update, optional push, build, and restart. The lock SHALL be released before the verb returns.

#### Scenario: Concurrent publish + reload on same source repo serialize

- **WHEN** `iris:publish` and `iris:reload` are invoked concurrently against the same source repo
- **THEN** the second call blocks until the first releases the lock; neither sees a partial state from the other

#### Scenario: Lock released on every failure path

- **WHEN** any step (ff merge, reset, push, build, restart) fails
- **THEN** iris writes a failure audit entry, releases the lock, and returns the structured error

### Requirement: Build and restart delegate to reload's helpers

`iris:publish` SHALL invoke the same build and restart implementations as `iris:reload` (`runBuildBlock` and `dispatchRestart`), reading the configuration from the source repo's `.iris.toml`. Output capture, timeout enforcement, and restart-mechanism dispatch SHALL be identical to reload.

#### Scenario: Build failure aborts before restart

- **WHEN** the configured build command exits non-zero
- **THEN** iris captures the combined output into `build_output`, writes a failure audit entry, releases the lock, and returns a structured error; the restart is NOT attempted

#### Scenario: Restart mechanism dispatch matches reload

- **WHEN** the source repo's `.iris.toml` declares `[restart] mechanism = "launchagent"` (or signal, exec, none, launchdaemon)
- **THEN** iris dispatches via the same `dispatchRestart` helper used by reload, returning the same output / warnings / errors

#### Scenario: Cross-repo restart works; self-publish refused for exit_code

- **WHEN** the source repo's `.iris.toml` declares `[restart] mechanism = "exit_code"` and the resolved source repo is iris's own deployed repo
- **THEN** iris refuses with a structured error stating that `exit_code` is only meaningful from `iris:reload` (publish is always cross-repo by construction – the worktree is the iris-managed task's workspace, not the iris source repo)

### Requirement: Audit log uses `mode: "publish"`

`iris:publish` SHALL append one JSON line per call to the same audit log used by `iris:reload` (`~/.iris/reload-history.jsonl`), with `mode = "publish"`. The schema SHALL match reload's `AuditEntry`; absent fields (e.g., `pulled`, which doesn't apply) are left at zero values.

#### Scenario: Success writes an audit entry with mode=publish

- **WHEN** a publish succeeds
- **THEN** iris appends a JSON line containing `{ timestamp, caller, target_source_repo, mode: "publish", pre_pull_sha, post_pull_sha, build_output, restart_mechanism, restart_output, restart_pending, outcome: "success" }` where `pre_pull_sha` is the source repo's HEAD before the update and `post_pull_sha` is the worktree HEAD that was applied

#### Scenario: Failure writes an audit entry with outcome=failure

- **WHEN** publish fails at any step
- **THEN** iris appends a JSON line with `mode: "publish"`, `outcome: "failure"`, and the error captured in `failure_reason`

#### Scenario: `iris ls` surfaces publish events alongside reloads

- **WHEN** the operator runs `iris ls`
- **THEN** publish entries appear in the same listing as reload entries, distinguishable by their `mode` value

### Requirement: Optional `--push` matches v1.0 `iris:push` guardrails

When `--push` is set, `iris:publish` SHALL push `<branch>` to origin from the source repo using the same guardrails as v1.0 `iris:push`: no force-push, default-branch refusal applies. Push happens AFTER the local ref update but BEFORE the build.

#### Scenario: Push runs after local update

- **WHEN** `iris:publish` is invoked with `push = true`
- **THEN** iris updates the local ref first (ff or reset), then runs `git push origin <branch>`, then builds + restarts; push failure aborts before the build runs

#### Scenario: Push refuses default branch even in publish

- **WHEN** the target branch equals the source repo's default branch and `push = true`
- **THEN** iris returns a structured error (same posture as `iris:push`) and does NOT push; the local update (ff or reset) has already happened

#### Scenario: Push failure surfaces git output

- **WHEN** `git push` exits non-zero (e.g., non-fast-forward, network failure)
- **THEN** iris returns a structured error containing git's stderr, writes a failure audit entry, and aborts before the build

### Requirement: `task_id` is required

`iris:publish` SHALL require `task_id`. The verb SHALL NOT support a no-arg / self-target invocation.

#### Scenario: Missing task_id is refused

- **WHEN** `iris:publish` is invoked with no `task_id`
- **THEN** iris returns a structured error naming the missing parameter

#### Scenario: Unknown task_id is refused

- **WHEN** `iris:publish` is invoked with a `task_id` that argus does not recognize
- **THEN** iris returns a structured error naming the task ID and performs no mutation

### Requirement: CLI mirrors MCP behavior

The plugin SHALL expose `iris publish <task_id> [--branch=X] [--push] [--reset]` as a cobra subcommand that calls the same `verbs.Publish` Go function as the MCP handler, against the live host shell, with the same structured result and exit-code-on-failure semantics.

#### Scenario: CLI invocation produces same structured result

- **WHEN** the user runs `iris publish <task-id> --reset --push` from any shell
- **THEN** the same `verbs.Publish` Go function executes and prints the structured result (JSON, by convention with other iris verbs); a failure returns a non-zero exit code
