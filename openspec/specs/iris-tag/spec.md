# iris-tag Specification

## Purpose
TBD - created by archiving change add-source-repo-utility-verbs. Update Purpose after archive.
## Requirements
### Requirement: `iris:tag` verb

The plugin SHALL expose `iris:tag` as an MCP tool and CLI subcommand that creates an annotated git tag at the SHA of `origin/<default-branch>` and pushes it to origin. The verb SHALL accept `task_id` (required, string), `tag` (required, non-empty string), and `message` (optional, string; defaults to `"Released by iris"`). The verb SHALL refuse if the tag already exists locally or on origin. On success, the verb SHALL return `{ tagged: true, tag, sha, message }`.

#### Scenario: Successful tag creates and pushes

- **WHEN** `iris:tag` is invoked with a new `tag` name
- **THEN** iris runs `git tag -a <tag> -m <message> origin/<default-branch>` followed by `git push origin <tag>`, and returns `{ tagged: true, tag, sha, message }`

#### Scenario: Default message used when message omitted

- **WHEN** `message` is empty or omitted
- **THEN** iris uses `"Released by iris"` as the annotated-tag message

#### Scenario: Refuses existing local tag

- **WHEN** `git rev-parse <tag>` succeeds locally (the tag already exists)
- **THEN** iris returns a structured error naming the existing tag and SHA, and does NOT attempt to create or push

#### Scenario: Refuses existing remote tag

- **WHEN** `git ls-remote --tags origin <tag>` returns a hit
- **THEN** iris returns a structured error naming the existing remote tag and its SHA

#### Scenario: Refuses empty tag

- **WHEN** `tag` is the empty string
- **THEN** iris returns a structured error before shelling out to git

#### Scenario: Non-zero git exit returns structured error

- **WHEN** either the tag-create or the push exits non-zero
- **THEN** iris returns a structured error carrying git's stderr; the tag MAY exist locally if the push step failed (caller's responsibility to clean up via `git tag -d <tag>` if desired)

#### Scenario: Refuses unknown task ID

- **WHEN** invoked with an unknown `task_id`
- **THEN** iris returns a structured error and does NOT shell out to git

#### Scenario: Refuses non-allowlisted source repo

- **WHEN** the resolved source repo is not on the argus project allowlist
- **THEN** iris returns a structured error naming the rejected path

#### Scenario: Per-source-repo lock held for create + push

- **WHEN** two concurrent `iris:tag` calls target the same source repo
- **THEN** the second blocks until the first releases the lock

#### Scenario: Direct CLI invocation mirrors MCP

- **WHEN** the user runs `iris tag <task-id> --tag <name> [--message "..."]`
- **THEN** the same `verbs.Tag` Go function executes and prints the structured result

