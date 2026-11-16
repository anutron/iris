## MODIFIED Requirements

### Requirement: Restart mechanism dispatch

Iris SHALL dispatch to the configured `[restart] mechanism`. v1 supports six mechanisms: `exit_code`, `launchagent`, `launchdaemon`, `signal`, `exec`, `none`. Unsupported values SHALL be refused at parse time.

#### Scenario: exit_code dispatches to self-exit

- **WHEN** `mechanism = "exit_code"` with default `code = 75` on a self-reload
- **THEN** iris responds to the caller, releases the lock, sleeps briefly to ensure the response is flushed, and calls `os.Exit(75)`

#### Scenario: launchagent runs `launchctl kickstart -k gui/<uid>/<label>`

- **WHEN** `mechanism = "launchagent"` with `label = "com.example.x"`
- **THEN** iris runs `launchctl kickstart -k gui/<uid>/com.example.x`, captures stdout+stderr into `restart_output`, and returns success when launchctl exits 0

#### Scenario: launchdaemon warns when iris is not root

- **WHEN** `mechanism = "launchdaemon"` and iris's effective UID is not 0
- **THEN** iris emits a warning in `warnings` naming the elevation issue but still attempts the kickstart

#### Scenario: signal reads pid_file and sends signal

- **WHEN** `mechanism = "signal"` with `pid_file = "/tmp/foo.pid"` and `signal = "SIGTERM"`
- **THEN** iris reads the PID from the file, sends SIGTERM via `syscall.Kill`, and returns the result of the kill call in `restart_output`

#### Scenario: exec runs the configured argv

- **WHEN** `mechanism = "exec"` with `command = ["systemctl", "--user", "restart", "foo"]`
- **THEN** iris runs the argv (no shell), captures stdout+stderr into `restart_output` without using pipes that a detached child could hold open, and enforces `timeout_seconds` (default 180)

#### Scenario: none is a no-op

- **WHEN** `mechanism = "none"`
- **THEN** iris records `restart_output = ""` and skips any restart action

#### Scenario: exec timeout reports deadline and partial output

- **WHEN** an exec restart exceeds its timeout
- **THEN** iris kills the command's process group, returns promptly even if a descendant holds stdout open, and the error names the deadline and includes the output captured so far
