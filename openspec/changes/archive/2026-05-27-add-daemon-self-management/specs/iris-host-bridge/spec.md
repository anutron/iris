# iris-host-bridge Specification

## MODIFIED Requirements

### Requirement: Source-repo path resolution from `task_id`

Every verb SHALL resolve the canonical source repo by calling argus's `GET /api/tasks/:id` to read the worktree path and then deriving the source repo via `git -C <worktree> rev-parse --git-common-dir`. Verbs SHALL NOT accept agent-supplied filesystem paths. Both the resolved source-repo path AND the worktree path SHALL be canonicalized (symlinks resolved, absolute) before any comparison or use as a lock key.

A narrow exception applies to **self-hosting verbs**: verbs whose normal operating mode is to act on iris itself (the running daemon's own source repo) MAY omit `task_id`. When invoked without `task_id` (and without an alternative explicit target such as `path`), a self-hosting verb SHALL discover its source repo via `os.Executable()` symlink resolution followed by a walk-up to the nearest `.git` directory. The result SHALL be canonicalized. The set of self-hosting verbs SHALL be enumerated in this requirement; verbs not in the list SHALL continue to require `task_id`.

In v1.1, the self-hosting verbs are: `iris:reload`, `iris:validate_config`, `iris:ls`, `iris:status`. All other verbs continue to require `task_id` as before.

#### Scenario: Verb refuses an unknown task ID

- **WHEN** a verb is invoked with a `task_id` that argus does not recognize (404)
- **THEN** the verb returns a structured error that names the task ID and performs no filesystem mutation

#### Scenario: Verb refuses a source repo outside the project allowlist

- **GIVEN** argus exposes `GET /api/projects/full` returning the operator-curated project list
- **WHEN** the resolved source-repo path does not match any project's canonicalized `path`
- **THEN** the verb returns a structured error naming the rejected path and performs no filesystem mutation

#### Scenario: Resolved paths are canonical on both sides

- **GIVEN** macOS resolves `/var` to `/private/var` via a system symlink
- **WHEN** argus stores the task's `worktree_path` as `/var/folders/.../wt` (non-canonical) and the source repo's working tree resolves to `/private/var/folders/.../src`
- **THEN** `verbs.Resolve` returns both `WorktreePath` and `SourceRepo` in their canonical (symlink-resolved, absolute) form so per-worktree and per-source-repo lock keys collide reliably for the same physical path

#### Scenario: Self-hosting verb without task_id discovers via os.Executable

- **GIVEN** the iris daemon is running as `/Users/aaron/.iris/irisd`, a symlink to `/Users/aaron/Development/Personal/iris/bin/iris`
- **WHEN** a self-hosting verb (e.g., `iris:reload`) is invoked with no `task_id`
- **THEN** iris resolves `os.Executable()`, follows the symlink chain to `/Users/aaron/Development/Personal/iris/bin/iris`, walks up to find the nearest `.git` directory at `/Users/aaron/Development/Personal/iris/.git`, and uses `/Users/aaron/Development/Personal/iris` (canonicalized) as the source repo

#### Scenario: Non-self-hosting verb still refuses missing task_id

- **WHEN** a verb not on the self-hosting list (e.g., `iris:push`, `iris:merge_to_master`) is invoked without `task_id`
- **THEN** the verb returns a structured error stating `task_id` is required, even if iris's own source repo would otherwise be derivable

#### Scenario: Self-hosting verb with both task_id and self-discovery inputs is ambiguous

- **WHEN** a self-hosting verb is invoked with both `task_id` and `path` set (where `path` is the optional explicit target)
- **THEN** the verb returns a structured error and resolves nothing

#### Scenario: Self-hosting verbs skip argus project allowlist for self-targets

- **WHEN** a self-hosting verb resolves its source repo via `os.Executable()` (self-target)
- **THEN** the verb does NOT consult argus's `/api/projects/full` (iris's own repo is implicit; the verb is constrained by being self-hosting, not by the allowlist)
- **AND** when the same verb is invoked with `task_id` or `path` resolving to a NON-self source repo, the argus allowlist enforcement applies as for any other verb
