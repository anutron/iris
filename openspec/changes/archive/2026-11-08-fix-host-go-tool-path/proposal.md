## Why

Iris's loaded LaunchAgent PATH omits the user's Go install directory. Installed tools such as goimports, golangci-lint, govulncheck, and gotestsum cannot resolve from host builds and checks, even though Go itself resolves. The audit log does not establish a specific historical Go-tool failure.

## What changes

- Resolve the user's Go binary directory through Go's GOBIN, then GOPATH/bin, then HOME/go/bin fallback.
- Append it to host command PATH while preserving existing executable precedence.
- Use the augmented environment for both direct executable lookup and descendants across host verbs.
- Warm discovery in the background at startup; cold calls immediately use exported directories or HOME/go/bin and never wait for Go.
- Cover sparse launchd environments with isolated fake tools, without modifying or restarting real services.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `iris-host-bridge`: Host commands consistently resolve installed Go tools.

## Impact

Shared subprocess construction and its callers in verbs and secret resolution. No public API changes or LaunchAgent edits. Deployment and daemon restart remain outside this change.
