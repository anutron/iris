## MODIFIED Requirements

### Requirement: `iris:run_checks` verb

The plugin SHALL expose `iris:run_checks` as an MCP tool and CLI subcommand accepting `task_id` (string, required), `check` (string, required, non-empty) and `args` (list of strings, optional). The verb SHALL resolve the task's worktree and run the executable `script/iris-check <check> [args...]` there under the per-worktree mutex, passing `check` and each element of `args` as its own argv element, in order, and never as a shell string. Iris SHALL NOT interpret, expand or glob any arg. It SHALL return `{command, exit_code, output}` with the combined output, where `command` shows `script/iris-check`, the check and the args. There SHALL be no Makefile fallback. On a non-zero exit, the MCP response SHALL be an error carrying the command, exit code and full output. The MCP tool SHALL follow the worktree-runs contract for detached execution, `wait_seconds` and `run_id`.

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

#### Scenario: Args are passed as separate argv entries

- **WHEN** the verb is invoked with `check="test"` and `args=["spec/models/user_spec.rb", "spec/a b.rb"]`
- **THEN** the script receives exactly three arguments, `test`, `spec/models/user_spec.rb` and `spec/a b.rb`, and the returned `command` is `script/iris-check test spec/models/user_spec.rb spec/a b.rb`

#### Scenario: Shell metacharacters in an arg stay literal

- **WHEN** the verb is invoked with `args=["spec/x.rb; touch pwned", "$(id)"]`
- **THEN** the script receives those two strings verbatim as single arguments and no shell interprets them

#### Scenario: Omitted args behave as before

- **WHEN** the verb is invoked without `args`, or with `args=[]`
- **THEN** the script is run with only the check argument, exactly as before this requirement

## ADDED Requirements

### Requirement: `args` are validated before anything runs

Iris SHALL reject the call with an error naming the offending arg, and run nothing, when `args` contains an element that starts with `-`, contains a NUL byte, or is the empty string, when it has more than 64 elements, or when any element is longer than 512 bytes. Validation SHALL happen before the worktree lock is taken and before any run is registered.

#### Scenario: Option-like arg is rejected

- **WHEN** the verb is invoked with `args=["--require", "evil.rb"]`
- **THEN** iris returns an error saying an arg may not start with `-`, naming `--require`, and runs nothing

#### Scenario: NUL byte is rejected

- **WHEN** an arg contains a NUL byte
- **THEN** iris returns an error saying an arg may not contain a NUL byte, and runs nothing

#### Scenario: Empty arg is rejected

- **WHEN** the verb is invoked with `args=["spec/a_spec.rb", ""]`
- **THEN** iris returns an error saying an arg may not be empty, and runs nothing

#### Scenario: Too many args

- **WHEN** the verb is invoked with 65 args
- **THEN** iris returns an error stating the limit of 64 args, and runs nothing

#### Scenario: Arg too long

- **WHEN** an arg is 513 bytes long
- **THEN** iris returns an error stating the limit of 512 bytes per arg, and runs nothing

#### Scenario: Limits are inclusive

- **WHEN** the verb is invoked with exactly 64 args, each exactly 512 bytes and none starting with `-`
- **THEN** iris runs the check normally
