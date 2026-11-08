## ADDED Requirements

### Requirement: Go tools in host command PATH

Every iris host command SHALL receive a PATH that includes the user's Go install directory. Cold calls SHALL immediately use exported GOBIN, otherwise the first exported GOPATH entry plus `/bin`, otherwise `$HOME/go/bin`, without waiting for Go. Iris SHALL warm bounded Go directory discovery in the background at daemon startup. Completed discovery SHALL refine later calls from `go env GOBIN`, otherwise the first `go env GOPATH` entry plus `/bin`, otherwise `$HOME/go/bin`. Discovery SHALL NOT depend on a managed repository's module or toolchain. Existing PATH order SHALL be preserved, and the directory SHALL NOT be duplicated. Direct executable lookup SHALL use the same augmented PATH as command descendants. Environment overlays and existing command execution controls SHALL be preserved. Relative PATH lookup SHALL retain Go's ErrDot protection.

#### Scenario: Go install directory configured

- **WHEN** Go reports a nonempty GOBIN, or an empty GOBIN and nonempty GOPATH
- **THEN** later configured builds, checks, restarts, hooks, git and gh subprocesses can invoke a tool installed in GOBIN or the first GOPATH entry's bin directory both directly and from a child shell

#### Scenario: Go unavailable under launchd

- **WHEN** Go cannot be queried and no exported Go directory exists
- **THEN** iris appends HOME/go/bin to the command PATH and tools installed there resolve without a login shell

#### Scenario: Existing executable wins

- **WHEN** a command already resolves through inherited or configured PATH
- **THEN** that executable retains precedence over a same-named tool in the appended Go bin directory

#### Scenario: Explicit child environment

- **WHEN** a build overlays environment variables or a verb injects secrets
- **THEN** iris preserves those entries and augments the final child PATH before resolving the command

#### Scenario: Command canceled during discovery

- **WHEN** a caller's deadline expires while the shared Go directory query is running
- **THEN** iris returns promptly under the caller's cancellation and the bounded query cannot delay that command's timeout

#### Scenario: Go discovery hangs on a cold process

- **WHEN** Go discovery hangs on the first verb invocation
- **THEN** the verb immediately proceeds using the cheap fallback PATH and can resolve a tool in HOME/go/bin without waiting for discovery
