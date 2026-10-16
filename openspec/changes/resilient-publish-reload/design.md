## Context

Publish resets or fast-forwards the host checkout before building. Both publish and reload currently use the request context throughout, so an MCP timeout kills the build or prevents restart after mutation. Existing push/fetch verbs already use `context.WithoutCancel` for host-owned operations; status already reads the latest durable audit entry.

## Goals / Non-goals

- Finish the locked sequence independently of caller deadlines, while keeping real command failures and configured step timeouts effective.
- Preserve synchronous results for callers that wait, and expose terminal restart outcome to later status calls.
- Do not add a job service or change daemon lifecycle during validation.

## Decisions

- Check cancellation after acquiring the lock, then detach with `context.WithoutCancel` before any checkout/reset/merge. This closes cancellation races during mutation itself and avoids starting queued work whose caller already cancelled. All subsequent reads, build, restart, verify, and audit remain part of that operation.
- Keep build, restart, and hook timeouts scoped separately. A build deadline must still prevent restart; it must not consume the restart's own budget. Bound detached git commands individually using the existing transfer timeout configuration, including optional publish push and reload fetch.
- Reuse the append-only audit and `iris_status.last_reload` instead of async job IDs. Add `restart_attempted` and `restart_succeeded` fields on terminal records. These describe dispatch, not service health; verification remains separately reported. Mechanism `none` counts as successful no-op dispatch, and self exit records remain pending until the scheduled exit.
- Keep pre-flight cancellation and genuine failures effective. Publish push failures still abort before build as required by existing specs.

## Risks / Trade-offs

- Detached work survives caller cancellation, not daemon termination. The existing best-effort audit remains unavailable if its filesystem cannot be written; write failures are logged.
- Status shows the previous terminal entry while work is still running. Callers must compare caller, timestamp, and post-update SHA rather than treat an old entry as confirmation.
- No live daemon deployment is performed by this change. Rollback is reverting the code and redeploying through the normal human-controlled workflow.
