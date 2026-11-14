## Why

Large check logs exceed Argus's 4 MiB plugin response limit. Its proxy truncates JSON before decoding, so callers get `decode plugin response: unexpected EOF` instead of a check outcome.

## What changes

- Bound check output at 64 KiB, preserve oversized combined logs in private host files, and return truncation metadata.
- Include bounded RSpec counts and failure excerpts when recognized, without changing small-result responses or MCP failure semantics.
- Apply the same result to inline completion and later `iris_run_result` polling, including captured output from timeouts.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `iris-run-checks`: Bound large output and retain full logs on the host.
- `iris-worktree-runs`: Polling returns the same bounded check result as inline completion.

## Impact

Go check verb and MCP handler, synthetic large-log regression tests, tool documentation. Argus proxy diagnostics require a separate change in the Argus repo. No deployment or reload is part of this change.
