## ADDED Requirements

### Requirement: Configurable, request-decoupled timeout for git-network verbs

Verbs whose core operation is a git network transfer (currently `iris:push`, `iris:fetch`) SHALL run that transfer under a timeout owned by iris rather than inheriting the caller-supplied context's cancellation or deadline. The transfer's context SHALL be derived as `context.WithTimeout(context.WithoutCancel(ctx), timeout)`, so cancellation of the inbound MCP request context (e.g. because argus's outbound client gave up waiting) does not terminate an in-flight transfer, while any context values on `ctx` remain available.

The timeout SHALL be configurable via a single shared `.iris.toml` field, `git_transfer_timeout_seconds` (`kind:"shared"`; checked into `.iris.toml`, identical for every developer and agent working on the project — not a per-developer workflow preference like `dogfood_branch` or `ship_ci_timeout_seconds`). It SHALL default to 300 seconds when unset, and SHALL be rejected as invalid when negative. A missing or unparseable `.iris.toml` SHALL NOT block the git-network verb — the timeout resolver SHALL fall back to the default rather than refusing the operation.

Only the transfer invocation itself adopts this timeout; other git invocations a verb performs (branch/remote resolution, rev-parse, ref snapshots) are fast local operations and SHALL continue to run under the caller's context unchanged.

#### Scenario: Git-network transfer outlives a cancelled caller context

- **WHEN** a git-network verb's transfer subprocess is in flight and the caller-supplied `ctx` is cancelled
- **THEN** the subprocess is NOT killed by that cancellation and the transfer runs to completion (success or its own timeout)

#### Scenario: Configured timeout is honored

- **WHEN** the source repo's `.iris.toml` sets `git_transfer_timeout_seconds = N`
- **THEN** the git-network verb's transfer is bounded by approximately `N` seconds, not the default and not the caller's context lifetime

#### Scenario: Default applies when unset or unreadable

- **WHEN** `.iris.toml` is absent, unreadable, or does not set `git_transfer_timeout_seconds`
- **THEN** the git-network verb's transfer is bounded by the default of 300 seconds

#### Scenario: Negative timeout is a validation error

- **WHEN** `.iris.toml` sets `git_transfer_timeout_seconds` to a negative number
- **THEN** `iris:validate_config` reports a validation error naming the field
