## Context

Git transfer verbs already detach their subprocesses, but synchronous MCP handlers can exceed both argus's callback deadline and iris's listener write timeout. The shared registry already solves this for worktree checks and builds.

## Goals / non-goals

Return pollable handles for slow MCP transfers while preserving existing final responses and classification. Part B adds per-repo opt-in HTTPS auth; reliable detection of interactive prompts remains excluded. No real token or host configuration is changed by implementation.

## Decisions

- Share the daemon's existing registry across checks, builds, push and fetch. Resolve before detaching to seed repo attribution; keep verb-level validation. Use the existing 20-second default and 25-second maximum wait, with an overall 25-second handler context so preflight also counts against the callback budget. The registry detaches from that context before executing the verb.
- Include all push options in a JSON deduplication key; exclude wait duration. Fetch has no transfer options. Notify the registry after taking the source lock, preserving queued/running semantics.
- Preserve the registry's in-memory lifecycle. An unknown transfer outcome requires remote-state verification before retry; change unknown-run guidance to cover transfers safely.
- Keep completed response encoding unchanged. Raising HTTP timeouts was rejected because it leaves callers waiting and does not address shorter upstream deadlines.
- Use local scalar `git_auth_token_source` plus existing `[secrets.op]` bootstrap. Load auth settings independently of build/restart validity, including repos lacking a shared config. Malformed config fails closed. Expose a single-source wrapper around the existing resolver; do not use its fail-open bulk env API.
- Use command-only `url.<https>.insteadOf` and `pushInsteadOf` for the effective remote's fetch/push URL so git still updates that named remote's tracking refs. Clear configured credential helpers, install a static shell helper reading a dedicated subprocess environment variable, and disable terminal/askpass prompts. Never put token values in argv or files. Redact captured output before errors or results leave the subprocess wrapper. Reject non-GitHub, embedded-credential and differing push-URL destinations in opt-in mode rather than redirecting writes.

## Risks / trade-offs

- Restart loses handles and may interrupt a transfer → explicitly report unknown outcome and require state verification.
- A run can wait behind another source operation → expose queued status; preserve existing per-source locking.
- A handle cannot explain whether SSH awaits approval → report running without guessing an auth state.
