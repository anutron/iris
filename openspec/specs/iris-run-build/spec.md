# iris-run-build Specification

## Purpose
TBD - created by archiving change add-run-build-verb. Update Purpose after archive.
## Requirements
### Requirement: `iris:run_build` verb

The plugin SHALL expose `iris:run_build` as an MCP tool accepting `task_id` (string, required) and `target` (string, optional). The verb SHALL resolve the argus task's worktree, discover the build command by convention, run the build in the worktree under a per-worktree mutex, and return a structured result containing the command, exit code, and combined output. On non-zero exit the verb SHALL return BOTH the populated result AND a typed error so callers see the captured output.

#### Scenario: Successful build via `script/iris-build`

- **WHEN** the verb is invoked for a task whose worktree contains an executable `script/iris-build`
- **THEN** iris runs `script/iris-build [target]` in the worktree and returns `{command: "script/iris-build [target]", exit_code: 0, output: "<combined stdout+stderr>"}`

#### Scenario: Successful build via Makefile fallback

- **WHEN** the verb is invoked for a task whose worktree has no `script/iris-build` but has a `Makefile` with a `build` target
- **THEN** iris runs `make build [target]` in the worktree and returns `{command: "make build [target]", exit_code: 0, output: "<combined stdout+stderr>"}`

#### Scenario: Neither build mechanism available

- **WHEN** the verb is invoked for a task whose worktree has neither an executable `script/iris-build` nor a `Makefile`
- **THEN** iris returns a structured error naming BOTH `script/iris-build` and the Makefile path so the operator knows how to opt in

#### Scenario: Non-zero exit returns result and typed error

- **WHEN** the build command exits with a non-zero status code (compile error, test failure, etc.)
- **THEN** iris returns BOTH a populated `*RunBuildResult{Command, ExitCode, Output}` AND a typed `*BuildExitError` wrapping that result, so callers using `errors.As` can access the output and exit code

#### Scenario: `target` argument is passed through

- **WHEN** the verb is invoked with `target="release"` and the worktree has `script/iris-build`
- **THEN** the script is invoked with `release` as its first argument and the script's interpretation of that target is preserved in the output

#### Scenario: Refuses an unknown task ID

- **WHEN** the verb is invoked with a `task_id` that argus does not recognize
- **THEN** iris returns a structured error naming the task ID and runs no build

#### Scenario: Refuses a source repo outside the project allowlist

- **WHEN** the resolved source-repo path does not match any allowlisted argus project
- **THEN** iris returns a structured error naming the rejected path and runs no build

#### Scenario: Concurrent builds in different worktrees run in parallel

- **WHEN** two `iris:run_build` calls fire concurrently for two different argus tasks whose worktrees are distinct (even if they share a source repo)
- **THEN** the builds run in parallel (the per-worktree mutex is keyed on worktree path, not source repo)

#### Scenario: Concurrent builds in the same worktree serialize

- **WHEN** two `iris:run_build` calls fire concurrently for the same argus task (or two tasks pointing at the same worktree)
- **THEN** the second call blocks until the first completes (the per-worktree mutex serializes writes to the same build directory)

#### Scenario: Direct CLI invocation runs the same verb

- **WHEN** the user runs `iris run-build <task-id> [target]` from any shell on the host
- **THEN** the same `verbs.RunBuild` Go function executes (bypassing the daemon process) and prints the structured result

### Requirement: Secrets resolution before build subprocess exec

`RunBuild` SHALL load `.iris.local.toml`'s `[secrets]` block for the resolved source repo (via
`config.LoadOverlay`) and inject every resolvable `[secrets.env]` mapping into the build
subprocess's own environment before it executes, whether the build runs via `script/iris-build` or
the Makefile fallback. A missing `.iris.toml`/`.iris.local.toml`, or a `[secrets]` block that is
absent or empty, SHALL leave the build's environment unchanged from today.

#### Scenario: Resolved secrets reach the build subprocess via `script/iris-build`

- **GIVEN** `.iris.local.toml` declares `[secrets.env] FOO = "env://FOO_SOURCE"` and
  `FOO_SOURCE` is set in iris's own process environment
- **WHEN** `iris:run_build` runs `script/iris-build`
- **THEN** the subprocess's environment includes `FOO=<resolved value>`

#### Scenario: Resolved secrets reach the build subprocess via the Makefile fallback

- **GIVEN** the worktree has no `script/iris-build` but has a `Makefile`, and `.iris.local.toml`
  declares a resolvable `[secrets.env]` mapping
- **WHEN** `iris:run_build` runs `make build`
- **THEN** the `make` subprocess's environment includes the resolved mapping

#### Scenario: No config present changes nothing

- **WHEN** the resolved source repo has no `.iris.toml`/`.iris.local.toml` at all
- **THEN** `iris:run_build` runs exactly as it did before this feature existed — no secrets
  resolution is attempted, and the build is not blocked by the absence of config

#### Scenario: An unresolved secret does not block the build

- **GIVEN** `.iris.local.toml` declares a `[secrets.env]` mapping whose source fails to resolve
- **WHEN** `iris:run_build` runs
- **THEN** the build still runs, with that one target variable left unset and a warning logged
  naming the variable and its source descriptor

