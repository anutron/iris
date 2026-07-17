## Context

`iris:push` (`internal/verbs/push.go`) and `iris:fetch` (`internal/verbs/fetch.go`) both call `runGit(ctx, sourceRepo, args...)` for their actual network operation, where `ctx` is whatever the caller passed in. On the real (MCP) path, that's `internal/mcp/handler_push.go:33` / `handler_fetch.go:30` passing `r.Context()` straight through — the inbound HTTP request context for argus's POST into iris's callback server. `runGit` feeds that context directly into `exec.CommandContext` (`internal/verbs/resolve.go:157`), so cancelling `ctx` SIGKILLs the git subprocess.

`r.Context()` is cancelled when the underlying connection closes — which happens when argus's outbound HTTP client gives up waiting on iris and aborts the request. iris has no visibility into, or control over, that client-side timeout. For a large push or a branch that's badly diverged from origin, the transfer can legitimately take longer than argus is willing to wait, and the git subprocess gets killed mid-flight as a side effect — not because iris decided the operation had run too long, but because a request-response cycle it doesn't own gave up. The caller sees a bare "context deadline exceeded" with no way to tell that apart from a real auth or network failure.

Separately, iris's own MCP callback server (`internal/mcp/server.go:70-71`) sets `WriteTimeout: 30s` on its `http.Server`. That's a fixed, iris-owned ceiling on how long a single request-response round trip can take before iris's own listener forcibly closes the connection — independent of anything discussed here. It is out of scope for this change (see Non-Goals and Risks below), but worth naming so the two timeouts aren't conflated.

## Goals / Non-Goals

**Goals:**

- Decouple the git push/fetch subprocess's lifetime from the inbound request's cancellation, so argus's client giving up does not kill an otherwise-healthy transfer.
- Give the transfer its own, iris-owned deadline instead of "however long the caller's context happens to live" — configurable per project, with a sane default.
- Classify a push/fetch failure into a small, stable set of reasons (`timeout`, `auth_failure`, `network_failure`, `other_failure`) so a caller — human or agent — can decide whether to retry, wait, or fall back to a manual push, instead of pattern-matching an opaque string.
- Keep the blast radius inside the git push/fetch invocation itself, plus the read-back immediately following a successful one. Git calls that run *before* any mutation is attempted (branch/remote lookups, the pre-fetch ref snapshot) are fast local operations and correctly stay on the caller's context — if it's already dead, aborting before mutating is right. Git calls that run *after* a successful mutation, purely to report what already happened (push's rev-parse of the resulting SHA; fetch's post-fetch ref snapshot), are also detached from the caller's context (see the dedicated decision below) — otherwise a caller's context dying in that exact instant would turn a genuine success into a reported failure.

**Non-Goals:**

- Not touching `internal/mcp/server.go`'s `WriteTimeout`. Even with this change, a push/fetch that legitimately runs past 30s will still see its HTTP response write fail once iris's own listener enforces that deadline — the git operation itself will have already completed (or kept running) under its own decoupled timeout by then, so the *work* isn't lost, but the *response* to that specific MCP call may not reach argus. Widening `WriteTimeout` (or otherwise decoupling the callback response from operation completion, e.g. an async/poll model) is a separate, larger change to the MCP transport that all `iris:*` verbs share, not something to fold into a push/fetch-scoped fix. Named as a known limitation below.
- Not adding a per-call timeout override (MCP input field or CLI flag). The knob is a project-wide `.iris.toml` setting; see the shared-vs-local decision below for why.
- Not attempting to determine, on a timeout, whether the push/fetch actually completed server-side before iris's local process was killed. That ambiguity is real (see Risks) and is exactly why the caller gets an explicit `timeout` classification instead of a bare error — the recommended recovery is to check state (`iris:fetch`, `iris:status`) rather than assume either outcome.
- Not reclassifying every possible git failure mode. `auth_failure` and `network_failure` are pattern-matched against git's stderr for the common, well-known cases; anything unrecognized (including non-fast-forward, unknown ref, etc.) is `other_failure`. This is deliberately conservative — a wrong classification is worse than an honest "other."

## Decisions

### Detach the transfer's context via `context.WithoutCancel`, not a bare `context.Background()`

**Decision:** The git push/fetch invocation runs under `context.WithTimeout(context.WithoutCancel(ctx), timeout)` — a context that inherits any values on the caller's `ctx` (e.g. `handler_reload.go`'s `callerKey`) but neither its cancellation nor its deadline.

**Rationale:** `context.WithoutCancel` (Go 1.21+, and this module targets Go 1.25) is the standard-library-blessed way to say "detach from this context's lifecycle, keep its values." Using a bare `context.Background()` instead would work today (no verb in this codebase currently threads request-scoped values through `ctx.Value`), but it's a silent trap for the next context value that gets added upstream — `WithoutCancel` documents the intent (detach lifecycle, keep data) directly in the code rather than relying on "nothing uses ctx.Value today" staying true.

**Alternatives considered:**

- *`context.Background()` directly* — simpler, but discards any future context values without saying so. Rejected as a foot-gun.
- *Leave `ctx` as-is, just don't wire `r.Context()` into the handler* — would require every call site (handler, CLI) to independently construct a fresh top-level context, duplicating the decoupling logic instead of centralizing it in the verb. Rejected; centralizing in `runGitTransfer` means the handler and CLI paths get the fix for free.

### `cmd.WaitDelay` is required, not optional — context cancellation alone does not bound a transfer with a subprocess tree

**Decision:** `runGitTransfer`'s `exec.Cmd` sets `WaitDelay: 1 * time.Second` in addition to the context timeout.

**Rationale:** This was discovered empirically while writing the timeout test, not designed up front. `git push`/`git fetch` to certain remotes (notably a local-path remote, which the test suite uses to simulate a slow/hung remote via a pre-receive hook or an `uploadpack` wrapper) spawn a second process (`receive-pack`/`upload-pack`) that inherits the parent's stdout/stderr pipe. `exec.CommandContext`'s default cancellation (`Cmd.Cancel`) only sends `Kill` to the *direct* child; it has no effect on that grandchild. When the context times out, the direct child dies, but if the grandchild is still running (e.g. mid a slow hook) it keeps the shared pipe open, and `Cmd.Wait()` — which reads that pipe to EOF — blocks until the grandchild eventually exits, potentially far past the configured timeout. This is documented explicitly in the `os/exec` docs for `Cmd.WaitDelay`: "If WaitDelay is zero (the default), I/O pipes will be read until EOF, which might not occur until orphaned subprocesses of the command have also closed their descriptors for the pipes." Setting `WaitDelay` bounds that extra wait and force-closes the pipes, so `runGitTransfer` actually returns at approximately the configured timeout instead of whenever the last orphaned subprocess happens to exit. Without this, the whole feature would be unreliable in exactly the scenario it exists for — a hung or slow-to-respond remote.

The `1 * time.Second` value is deliberately short: it only matters on the abnormal path (cancellation, or a genuinely hung process). On ordinary completion, the git process's own I/O closes immediately — well inside this window — so a short `WaitDelay` never truncates legitimate output.

**Alternatives considered:**

- *Rely on the context timeout alone* — this is what was tried first; the timeout test failed, taking the full duration of the hook's sleep instead of the configured timeout, which is what surfaced this issue.
- *Kill the whole process group (`Setpgid` + negative-PID signal)* — would also work and is the more "complete" fix (actually terminates the orphan rather than just detaching from its pipes), but changes process-group semantics for every `git push`/`git fetch` iris runs, which felt like a bigger and riskier change than bounding the wait. `WaitDelay` solves the actual problem (iris's own call returning on time) without needing to reach into and kill a process iris doesn't directly own.

### Read-back after a successful transfer is also detached, on a short fixed grace period

**Decision:** After `runGitTransfer` succeeds, the small local git read used to report the outcome — push's `git rev-parse <remote>/<effective-branch>`, fetch's post-fetch ref snapshot — also runs under `context.WithTimeout(context.WithoutCancel(ctx), postTransferReadTimeout)` (10s, a fixed constant, not the configurable `git_transfer_timeout_seconds`). Git calls that happen *before* any mutation (branch/remote resolution, the pre-fetch snapshot) are unaffected and still run on the caller's `ctx`.

**Rationale:** This was the second thing the tests caught: the first version of this change decoupled only the transfer itself, and a test that cancelled `ctx` right as the push completed saw `Push` return an error anyway — not from the push (which had genuinely succeeded), but from the trailing `rev-parse` reading back the result, which was still on the now-cancelled `ctx`. That's a real bug in spirit even though it's "just a read": a caller's context dying in the exact instant a mutation completes should not turn a real success into a reported failure. The fix is symmetric with the transfer's own decoupling, just with a small fixed timeout instead of a configurable one, since this is a fast local read whose duration has nothing to do with network conditions or repo size.

**Alternatives considered:**

- *Leave the read-back on `ctx`* — matches a narrower reading of "only the transfer is in scope," but produces a confusing outcome: the mutation succeeded, yet the verb reports failure, for a caller who (by definition, since `ctx` died) isn't even listening anymore. Rejected as a needless footgun for the same reason the transfer itself is decoupled.
- *Use the full `git_transfer_timeout_seconds` for the read-back too* — unnecessary; this is a local operation, not a network one, so it doesn't need a project-configurable ceiling. A short fixed constant is simpler and cannot be misconfigured.

### One shared `.iris.toml` field, not one-per-verb or a per-call override

**Decision:** A single new top-level field, `git_transfer_timeout_seconds`, used by both `iris:push` and `iris:fetch`. It is classified `kind:"shared"` (checked into `.iris.toml`, identical for every developer and agent), not `kind:"local"`.

**Rationale — why one field, not two:** Push and fetch are symmetric git-network-transfer operations; the same repo, hosted on the same remote, over the same network, has essentially the same "how long is a large transfer allowed to take" answer for both directions. Splitting into `push_timeout_seconds` / `fetch_timeout_seconds` doubles the config surface for a distinction with no concrete driving use case today. If one ever emerges, splitting later is an additive, backward-compatible change (an unset per-verb field falls back to the shared one).

**Rationale — why shared, not local:** The existing taxonomy (`add-iris-local-toml-overlay`'s design) classifies a field `local` when it's an inherently personal, per-developer workflow preference — `dogfood_branch` (which branch *you* compose onto) and `ship_ci_timeout_seconds` (how long *your* `ship_feature` invocation waits on CI) are both about an individual's workflow, not a fact about the repo. `git_transfer_timeout_seconds` is the opposite shape: it reflects the repo's own characteristics — its size, its remote host, its typical transfer volume — which are the same regardless of which developer or agent is pushing/fetching. That's the same shape as `[build] timeout_seconds` (nested under the shared `build` block): "how long does this project's build take" is a project fact, not a personal preference, even though it's *also* "just a timeout." Consistency with that precedent, not the mere presence of the word "timeout," is what puts this field on the shared side.

**Alternatives considered:**

- *`kind:"local"`, reasoning "it's a timeout, like `ship_ci_timeout_seconds`"* — rejected once the actual local/shared distinction (personal workflow vs. repo fact) is applied rather than pattern-matching on field shape.
- *Nested block (`[push]`/`[fetch]` tables, mirroring `[build]`/`[restart]`)* — considered for consistency with how `BuildBlock`/`HookBlock` group related settings, but there's only one scalar today; a bare top-level field (like `default_branch`, also shared) is simpler and avoids inventing an empty-looking table for a single field. If push/fetch config grows more knobs later, promoting to a block is a natural, additive refactor.
- *Per-call override on `PushOptions`/`FetchInput` (MCP input field or CLI flag)* — rejected per the proposal's scope: the task calls for a `.iris.toml`-configurable timeout, not a per-invocation one. A per-call override would also let an agent silently paper over a genuinely-too-small project default instead of fixing the config once for everyone.

**Default value:** `DefaultGitTransferTimeoutSeconds = 300` (5 minutes). Today's *effective* ceiling is undefined — it's whatever argus's outbound client timeout happens to be, entirely outside iris's control, and the whole point of this change is to stop depending on it. 300s is comfortably larger than typical push/fetch durations (including a large or far-diverged branch) while still bounded — in the same scale as `DefaultBuildTimeoutSeconds` (600s) and `DefaultShipCITimeoutSeconds` (600s), but shorter, since a git transfer is normally faster than a build or a CI run.

### Failure classification: a typed error, not a response-shape change

**Decision:** `runGitTransfer` returns a `*GitTransferError{Op, Reason, Timeout, Err}` on failure. `Reason` is one of `timeout` / `auth_failure` / `network_failure` / `other_failure`. `Error()` renders a reason-specific, human-and-agent-legible message (e.g., for `timeout`: names the configured deadline and states explicitly that this is iris's own timeout, not a network/auth failure). The verb wraps this the same way it already wraps `runGit` errors (`fmt.Errorf("push %s to %s: %w; log:\n%s", ...)`), so `errors.As` still reaches the `*GitTransferError` through the chain, and the MCP handler's existing `fmt.Sprintf("iris:push: %v", err)` picks up the classified message with zero handler-side code changes.

**Rationale:** The MCP tool-response envelope (`internal/mcp/envelope.go`) is a single text block with no structured side-channel — changing that shape would touch every verb's response, far outside this change's scope. Carrying the classification in the error *type* rather than the response *shape* means: (a) Go callers (tests, and any future Go-level consumer) get a precise, `errors.As`-checkable signal via `IsGitTransferTimeout(err)`; (b) MCP/CLI callers (today, exclusively text) get a clearly-worded, reason-prefixed message for free, through formatting that already exists.

**Classification method — timeout:** deterministic. After `runGit` returns an error, check the *transfer's own* context (`transferCtx.Err() == context.DeadlineExceeded`), not the process's exit error — this is the pattern Go's `os/exec` docs recommend, since the killed process's own error text ("signal: killed") doesn't reliably say why.

**Classification method — auth / network:** pattern-matched against git's combined stdout+stderr (already captured by `runGit`) against a small, explicit set of known substrings (`"permission denied"`, `"authentication failed"`, `"could not resolve host"`, `"connection refused"`, etc.). Anything that doesn't match either set — including non-fast-forward, unknown ref, and any git failure mode not explicitly recognized — falls through to `other_failure`. This is intentionally conservative: false "auth" or "network" labels are worse than an honest "other" that still carries the underlying git message verbatim.

**Alternatives considered:**

- *Only distinguish timeout vs. not* — simpler, and covers the deliverable's stated minimum ("distinguishes deadline vs other failures"), but the root-cause brief explicitly asks for auth/network distinction too, and the pattern-match cost is small and independently unit-testable (no live auth/network fixtures needed — synthetic stderr strings suffice).
- *Parse git's exit code* — git's exit codes are not granular enough to distinguish these cases (auth and network failures both typically exit 128, same as many other fatal errors). Rejected.

## Risks / Trade-offs

- **[Risk]** On a `timeout` classification, whether the push/fetch actually completed on the remote before iris's local process was killed is genuinely unknown — killing the client process doesn't guarantee the server-side operation (e.g., `receive-pack` after a pre-receive hook) didn't finish first. → **Mitigation:** the `timeout` error message explicitly recommends checking state (`iris:fetch`, `iris:status`) rather than blindly retrying; this is a caller-facing recommendation, not a guarantee iris can make mechanically without adding a post-timeout reconciliation step (out of scope here).
- **[Risk]** `internal/mcp/server.go`'s 30s `WriteTimeout` can still cut the HTTP response for a push/fetch that legitimately runs past 30s under its own (now longer, e.g. 300s default) timeout — the operation keeps running and can still succeed, but the calling agent's specific MCP round trip may see a transport error regardless of this fix. → **Mitigation:** none in this change; named explicitly as a Non-Goal. A follow-up would need to widen `WriteTimeout` or move to an async/poll model for long-running verbs generally (not push/fetch-specific).
- **[Trade-off]** Auth/network classification is heuristic (stderr substring matching), not exhaustive. → **Mitigation:** unmatched cases fall back to `other_failure` with the original git message intact, so nothing is lost — the classification is a helpful hint, not the sole source of truth.
- **[Trade-off]** A misconfigured (malformed) `.iris.toml` should not block a push/fetch — that's a `validate_config`/build-hook concern, not a git-connectivity one. → the timeout loader falls back to the default on any load error (missing file, parse error, or unreadable field), silently, mirroring the precedent in `LoadIrisToml`'s own doc comment ("callers that treat the config as optional... silently fall back to the no-config code path").

## Acceptance Criteria (Prove-It)

1. Cancelling the caller-supplied `ctx` while a `git push`/`git fetch` is in flight does not kill the subprocess; the operation completes, the result read-back also survives the cancelled `ctx`, and the verb reports success.
2. A configured `git_transfer_timeout_seconds` shorter than the actual transfer time causes `Push`/`Fetch` to fail with a `*GitTransferError{Reason: GitTransferTimeout}` at approximately the configured duration (not the default, not immediately, and not the full duration of a slow remote — see the `WaitDelay` decision).
3. Omitting `git_transfer_timeout_seconds` defaults to 300s.
4. A negative `git_transfer_timeout_seconds` is a validation error.
5. `git_transfer_timeout_seconds` is classified `shared` (pinned in the taxonomy exhaustiveness tests).
6. Synthetic git stderr containing known auth-failure substrings classifies as `auth_failure`; known network-failure substrings classify as `network_failure`; anything else classifies as `other_failure`.
7. Git invocations that happen *before* any mutation is attempted (branch/remote resolution, the pre-fetch snapshot) remain on the caller's context, unaffected by this change; git invocations that happen *after* a successful mutation to report its outcome (push's rev-parse, fetch's post-fetch snapshot) are detached from the caller's context on a short fixed grace period.
8. `cmd/iris/push.go`, `cmd/iris/fetch.go`, `internal/mcp/handler_push.go`, `internal/mcp/handler_fetch.go` require no code changes (verified by `go build ./...`); the classified error surfaces through their existing formatting.
