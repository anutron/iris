# iris-reload Specification

## ADDED Requirements

### Requirement: Optional caller-supplied build branch

`iris:reload` SHALL accept an optional caller-supplied build branch (an internal `ReloadInput` field, not a new MCP/CLI input). When empty — the case for every existing caller — reload behaves exactly as before, building and restarting against whatever branch is currently checked out. When non-empty, reload SHALL, after acquiring the per-source-repo lock and before loading `.iris.toml` and running the build, check out the named branch in the source repo, so the configuration load, build, and restart all act on that branch's tree (the composed SHA). After the build and restart steps — on every exit path, including build failure, restart failure, and the success path — reload SHALL restore the branch that was checked out when the call entered.

This requirement does NOT relax the entry pre-flight: reload still refuses a dirty working tree and still requires HEAD to be on the resolved default branch before it acquires the lock or mutates anything. The build-branch checkout happens only after those checks pass and the lock is held.

A failure to restore the entry branch SHALL be surfaced as a warning in the structured result (and audit entry) rather than swallowed; the restart itself is not rolled back (the new binary has already been deployed).

#### Scenario: Empty build branch preserves existing behavior

- **WHEN** `iris:reload` is invoked with no build branch (every existing caller)
- **THEN** reload builds and restarts against the currently checked-out branch and performs no extra checkout or restore

#### Scenario: Build branch is checked out for the build and restored after

- **GIVEN** a source repo on its default branch with a clean working tree, and a caller-supplied build branch whose tree differs from the default branch's tree
- **WHEN** `iris:reload` runs with that build branch
- **THEN** reload acquires the lock, checks out the build branch, loads `.iris.toml` and runs `[build] command` against the build branch's tree, dispatches the restart, and then checks the source repo back out on the branch it entered on (the default branch)

#### Scenario: Entry pre-flight still applies with a build branch

- **GIVEN** a caller-supplied build branch
- **WHEN** the source repo's working tree is dirty, or HEAD is not on the resolved default branch
- **THEN** reload refuses at pre-flight exactly as it would without a build branch — before acquiring the lock and before any checkout

#### Scenario: Restore failure surfaces a warning without rolling back the restart

- **GIVEN** a reload that checked out a build branch, built, and restarted successfully
- **WHEN** restoring the entry branch fails
- **THEN** reload returns success for the build and restart, appends a warning naming the branch it could not restore (and the branch the repo was left on), and does NOT undo the restart
