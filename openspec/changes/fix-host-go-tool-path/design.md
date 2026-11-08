## Context

The installed and loaded iris LaunchAgent includes Homebrew and user local bins but omits HOME/go/bin. All host commands currently inherit that environment; builds/checks sometimes overlay secrets or configured variables. Go's exec.CommandContext resolves names before cmd.Env is assigned, so changing only the child environment is insufficient. Argus's launchd gotchas document this same lookup trap.

## Goals / non-goals

- Resolve Go-installed executables directly and from descendants of every host command.
- Preserve existing PATH precedence, command arguments, cancellation, secrets, and timeout behavior.
- Leave real LaunchAgent files and running services untouched.

## Decisions

- Introduce one internal hostexec command constructor taking the complete optional child environment. Resolve names using the final PATH, then construct exec.Cmd with that environment. Route verbs and secret subprocesses through it.
- Discover GOBIN and GOPATH with a bounded, module-independent `go env` invocation. Prefer GOBIN; otherwise append bin to the first GOPATH entry; use HOME/go/bin if discovery is unavailable. Honor exported Go directories even when Go is unavailable.
- Append the Go directory without duplication. Existing PATH entries keep their precedence, including the current local-bin argus symlink.
- Cache discovery by relevant environment settings so repeated git/gh calls do not repeatedly spawn Go. Keep command environment and secrets out of the cache.
- Warm discovery asynchronously at daemon startup and coalesce queries by environment. Calls never wait: they use exported directories or HOME/go/bin until discovery completes, then use its refined result. A cold custom directory configured only through `go env -w` therefore becomes available on later calls; export GOBIN/GOPATH for immediate availability.
- Preserve Go's protection against resolving executables through relative PATH entries.

## Risks / trade-offs

- Go discovery can fail or be slow → bound it and fall back without blocking unrelated host commands.
- Go configuration can change while iris runs → environment changes select a fresh cache entry; changes to go env's persisted configuration require a new iris process.
- Explicit build PATH overrides need descendant tools too → augment the final overlaid environment and use it for executable lookup.
