## Context

The host uses a composite dogfood branch. Reload currently requires the default branch and restores it after building dogfood, creating a restart/checkout race. RunRegistry already supports detached operations and bounded transport waits.

## Goals / Non-goals

- Provide one lease-gated operation that builds, restarts, and verifies the requested dogfood tree.
- Preserve existing default-branch reload for projects without dogfood.
- Keep tests isolated to temporary repositories and fake services.
- Conflict resolution remains an agent responsibility; a failed merge is aborted.

## Decisions

- Deploy accepts `ref` and optional `compose`; composition merges the ref with `--no-ff` onto the current dogfood tip, preserving other workers' commits. Direct mode retains the ancestry guard and explicit force override.
- Hold the source lock over composition, branch movement, build, restart, and readiness. Reuse the lease gate over the full operation, including its BeginFlight/EndFlight lifecycle.
- Refactor reload/set_dogfood into lock-owning public wrappers and internal locked operations, avoiding gaps and recursive locking.
- Dogfood reload skips upstream mutation: callers compose explicitly. Build and restart use the same checkout, which remains dogfood on completion and failure.
- Shared `[dogfood_ready]` accepts ports, health URLs, timeout_seconds, and interval_seconds. Poll TCP localhost connections and HTTP 2xx; require every configured check in the same pass. Return evidence and timeout failures without rollback.
- MCP deploy/reload/set_dogfood share RunRegistry and wait_seconds clamped to 0–25 seconds. Operation identity excludes wait_seconds.
- Self-reload returns restart_pending and a warning; readiness cannot be verified after exit_code within the same daemon. In-memory runs disappear on iris restart.

## Risks / Trade-offs

- Open ports and HTTP success prove configured readiness, not process identity. Report the built commit as served only after readiness; document this evidence boundary.
- Build/restart failures leave dogfood selected for diagnosis; do not silently restore main.
- In-memory handles cannot survive self-restart; return the warning before the scheduled exit.
