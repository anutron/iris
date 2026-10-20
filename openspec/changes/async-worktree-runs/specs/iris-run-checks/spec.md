## ADDED Requirements

### Requirement: `iris:run_checks` verb

The plugin SHALL expose `iris:run_checks` as an MCP tool and CLI subcommand accepting `task_id` (string, required) and `check` (string, required, non-empty). The verb SHALL resolve the task's worktree and run the executable `script/iris-check <check>` there under the per-worktree mutex, passing `check` as a single argv element and never as a shell string. It SHALL return `{command, exit_code, output}` with the combined output. There SHALL be no Makefile fallback. On a non-zero exit, the MCP response SHALL be an error carrying the command, exit code and full output. The MCP tool SHALL follow the worktree-runs contract for detached execution, `wait_seconds` and `run_id`.

#### Scenario: Passing check

- **WHEN** `script/iris-check lint` exits 0
- **THEN** iris returns `{command: "script/iris-check lint", exit_code: 0, output: "<combined output>"}`

#### Scenario: Failing check carries its output

- **WHEN** `script/iris-check test` exits 1
- **THEN** the MCP response is an error containing `script/iris-check test exited 1` and the full output

#### Scenario: Missing check script

- **WHEN** the worktree has no executable `script/iris-check`
- **THEN** iris returns an error naming the expected path and runs nothing

#### Scenario: Empty check is rejected

- **WHEN** the verb is invoked with `check=""`
- **THEN** iris returns an error mentioning `check` and runs nothing
