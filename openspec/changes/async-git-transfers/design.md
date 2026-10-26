## Context

Git transfer verbs already detach their subprocesses, but synchronous MCP handlers can exceed both argus's callback deadline and iris's listener write timeout. The shared registry already solves this for worktree checks and builds.

## Goals / non-goals

Return pollable handles for slow MCP transfers while preserving existing final responses, classification and CLI behavior. Credential changes and reliable detection of interactive prompts are outside Part A.

## Decisions

- Share the daemon's existing registry across checks, builds, push and fetch. Resolve before detaching to seed repo attribution; keep verb-level validation. Use the existing 20-second default and 25-second maximum wait, with an overall 25-second handler context so preflight also counts against the callback budget. The registry detaches from that context before executing the verb.
- Include all push options in a JSON deduplication key; exclude wait duration. Fetch has no transfer options. Notify the registry after taking the source lock, preserving queued/running semantics.
- Preserve the registry's in-memory lifecycle. An unknown transfer outcome requires remote-state verification before retry; change unknown-run guidance to cover transfers safely.
- Keep completed response encoding unchanged. Raising HTTP timeouts was rejected because it leaves callers waiting and does not address shorter upstream deadlines.

## Risks / trade-offs

- Restart loses handles and may interrupt a transfer → explicitly report unknown outcome and require state verification.
- A run can wait behind another source operation → expose queued status; preserve existing per-source locking.
- A handle cannot explain whether SSH awaits approval → report running without guessing an auth state.
