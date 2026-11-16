## Why

`iris_dogfood_deploy` failed its reload with `exec [.../script/iris-restart]: context deadline exceeded` and left the dogfood stack down. The exec restart ran under a hard-coded 30s default, but real restart scripts (credential provisioning, port reclaim, relaunch) take longer. iris also captured output through a pipe, so a disowned relaunched child holding stdout open could keep the wait blocked past the deadline, and the timeout error did not say what the deadline was.

## What Changes

- The exec restart captures stdout+stderr to a temp file instead of a pipe, so a detached/backgrounded child can never block iris after the script itself exits.
- The default `[restart] timeout_seconds` for exec rises from 30 to 180 (still configurable).
- A timeout error names the deadline, hints at `timeout_seconds`, and includes the partial output captured so far.

## Impact

- Specs: iris-reload (exec scenario).
- Code: internal/verbs/reload.go (runArgv), internal/config/iris_toml.go.
