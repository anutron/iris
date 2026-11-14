## MODIFIED Requirements

### Requirement: `iris:run_checks` verb

The plugin SHALL expose `iris:run_checks` as an MCP tool and CLI subcommand accepting `task_id` (string, required), `check` (string, required, non-empty) and `args` (list of strings, optional). The verb SHALL resolve the task's worktree and run the executable `script/iris-check <check> [args...]` there under the per-worktree mutex, passing `check` and each element of `args` as its own argv element, in order, and never as a shell string. Iris SHALL NOT interpret, expand or glob any arg. It SHALL return `{command, exit_code, output}`, where `command` shows `script/iris-check`, the check and the args. Output up to 64 KiB SHALL be returned verbatim. Larger output SHALL be saved byte-for-byte in a private host log file and replaced by a UTF-8-safe tail of at most 64 KiB, with `output_truncated`, `output_bytes`, and `log_path` metadata. Recognized RSpec counts and a failure excerpt of at most 16 KiB SHALL be included for large logs, with explicit failure excerpt truncation. There SHALL be no Makefile fallback. On a non-zero exit, the MCP response SHALL remain an error carrying the command, exit code and bounded output; large errors SHALL also carry structured result metadata. On timeout or process-start failure, captured output SHALL use the same bounds and preservation policy, with exit code -1 and an actionable error. If the full log cannot be saved, the response SHALL clearly report the persistence failure. The MCP tool SHALL follow the worktree-runs contract for detached execution, `wait_seconds` and `run_id`.

#### Scenario: Passing check

- **WHEN** `script/iris-check lint` exits 0
- **THEN** iris returns `{command: "script/iris-check lint", exit_code: 0, output: "<combined output>"}`

#### Scenario: Failing check carries its output

- **WHEN** `script/iris-check test` exits 1
- **THEN** the MCP response is an error containing `script/iris-check test exited 1` and the full output when it is at most 64 KiB

#### Scenario: Large check result remains readable

- **WHEN** a check emits more than 4 MiB and exits 0 or non-zero
- **THEN** its result includes the exit code, bounded tail and log path, the log file contains all original bytes, and the encoded callback response is below 1 MiB

#### Scenario: RSpec failure appears before a long epilogue

- **WHEN** a large RSpec log has a Failures section and summary before a long output epilogue
- **THEN** recognized counts and a bounded failure excerpt remain available alongside the tail

#### Scenario: Full log cannot be saved

- **WHEN** the host log directory cannot be written for an oversized output
- **THEN** the response is a bounded error stating that preservation failed

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
