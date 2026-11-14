## Context

Iris captures combined output and embeds it whole in a JSON text block. Argus's plugin proxy decodes through `io.LimitReader(resp.Body, 4*1024*1024)`, causing unexpected EOF when the response exceeds that limit. There is no scanner on this response path.

## Goals / Non-goals

- Return reliable outcomes for synthetic multi-megabyte checks and preserve complete output.
- Preserve existing small-response shapes and error flags.
- Do not execute Sketch checks, deploy, reload, or modify Argus in this PR.

## Decisions

- Keep `command`, `exit_code`, and `output`. For output over 64 KiB, replace output with a UTF-8-safe tail and add `output_truncated`, `output_bytes`, and `log_path`. Store raw bytes under the host user cache's `iris/check-logs` directory using private permissions and unique names. Logs remain available beyond run-registry expiry; deletion is manual.
- Recognize RSpec summary counts and `Failures:` sections without assuming every check uses RSpec. Cap the failure excerpt at 16 KiB and report excerpt truncation. Unknown formats still expose exit code and tail.
- Large non-zero results keep the human-readable command/exit message and MCP `isError`, with a second structured block for metadata. Small failures retain their exact text shape.
- Preserve output on timeout too, returning exit code -1 with the timeout error. Log persistence failure returns a clear error rather than pretending the full log was saved.
- Bound the final encoded HTTP callback response at 1 MiB. If any other handler exceeds this budget, return a clear bounded MCP error before writing JSON, avoiding the known proxy cutoff.

## Risks / Trade-offs

- Host files consume disk until manually removed; the response names their location and documentation explains retention.
- Framework-specific parsing is best effort; process exit code remains authoritative.
- Capture still uses memory during execution; streaming execution logs is outside this fix.
