# iris-branch-delete-remote Specification

## Purpose
TBD - created by archiving change add-source-repo-utility-verbs. Update Purpose after archive.
## Requirements
### Requirement: `iris:branch_delete_remote` verb

The plugin SHALL expose `iris:branch_delete_remote` as an MCP tool and CLI subcommand that deletes a remote branch on origin via `git push origin :<branch>`. The verb SHALL accept `task_id` (required, string) and `branch` (required, non-empty string). The verb SHALL refuse to delete the resolved default branch and SHALL refuse non-existent branches. On success, the verb SHALL return `{ deleted: true, branch, prior_remote_sha }`.

#### Scenario: Successful delete returns prior remote SHA

- **WHEN** `iris:branch_delete_remote` is invoked with a branch that exists on origin and is NOT the default branch
- **THEN** iris captures the branch's SHA via `git ls-remote --heads origin <branch>`, runs `git push origin :<branch>`, and returns `{ deleted: true, branch, prior_remote_sha }`

#### Scenario: Refuses default branch

- **WHEN** `branch` equals the resolved default branch (from `git symbolic-ref refs/remotes/origin/HEAD`)
- **THEN** iris returns a structured error naming both the requested branch and the default

#### Scenario: Refuses empty branch

- **WHEN** `branch` is the empty string
- **THEN** iris returns a structured error before shelling out to git

#### Scenario: Refuses non-existent branch

- **WHEN** the branch does not exist on origin (no entry in `git ls-remote --heads origin <branch>`)
- **THEN** iris returns a structured error naming the missing branch

#### Scenario: Non-zero git exit returns structured error

- **WHEN** the `git push origin :<branch>` invocation exits non-zero
- **THEN** iris returns a structured error carrying git's stderr

#### Scenario: Refuses unknown task ID

- **WHEN** invoked with an unknown `task_id`
- **THEN** iris returns a structured error and does NOT shell out to git

#### Scenario: Refuses non-allowlisted source repo

- **WHEN** the resolved source repo is not on the argus project allowlist
- **THEN** iris returns a structured error naming the rejected path

#### Scenario: Per-source-repo lock held for push

- **WHEN** two concurrent `iris:branch_delete_remote` calls target the same source repo
- **THEN** the second blocks until the first releases the lock

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris branch-delete-remote <task-id> --branch <name>`
- **THEN** the same `verbs.BranchDeleteRemote` Go function executes and prints the structured result

