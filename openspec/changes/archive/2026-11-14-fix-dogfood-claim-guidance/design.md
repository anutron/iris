## Context

Wording-only fix; messages are plain string edits in `internal/verbs`, tool descriptions in `internal/daemon/run.go`.

## Decisions

- Name `iris_dogfood_deploy` in every agent-facing "what to do next" sentence; keep `iris_set_dogfood` described as lower-level.
- The config bootstrap doc keeps `iris_set_dogfood` because it deploys a finished, self-composed SHA.
