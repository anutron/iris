# iris-complete-task Specification

## Purpose
TBD - created by archiving change add-complete-task-verb. Update Purpose after archive.
## Requirements
### Requirement: `iris:complete_task` verb

The plugin SHALL expose `iris:complete_task` as an MCP tool accepting `task_id` (string, required) and `merge_strategy` (enum "no_ff"|"ff_only", default "no_ff"). The verb SHALL run a fixed five-step ship-it sequence and SHALL return the checkpoints reached so partial failures are diagnosable. The three git-mutating sub-steps (merge, default-branch push, remote-task-branch delete) SHALL be performed under a single per-source-repo mutex acquisition so no other iris verb can mutate the source repo between sub-steps. The argus state transitions (status update, archive) SHALL run after the mutex is released, because they do not touch the source repo.

#### Scenario: Happy full path completes all five checkpoints

- **WHEN** the verb is invoked for an in-progress task whose merge succeeds
- **THEN** iris executes (1) merge_to_master, (2) `git push origin <default>`, (3) `git push origin --delete <task-branch>`, (4) `POST /api/tasks/<id>/status {"status":"complete"}`, (5) `POST /api/tasks/<id>/archive`
- **AND** the response carries `checkpoints: ["merged", "default_branch_pushed", "remote_task_branch_deleted", "task_marked_complete", "task_archived"]`

#### Scenario: Already-complete task returns success immediately

- **WHEN** the verb is invoked for a task whose argus status is already `"complete"`
- **THEN** iris performs no work and returns all five checkpoints with no error

#### Scenario: Partial failure surfaces reached checkpoints

- **WHEN** any sub-step after merge fails (e.g., the status update returns HTTP 5xx)
- **THEN** iris returns a Go error wrapping the failed sub-step AND a result whose `checkpoints` list contains exactly the checkpoints reached before the failure

#### Scenario: Archive failure is non-fatal

- **WHEN** the archive sub-step (step 5) fails after every prior step succeeded
- **THEN** iris returns no Go error, the result carries the first four checkpoints, and `result.error` is a string describing the archive failure

#### Scenario: Remote branch already deleted is treated as success

- **WHEN** the remote-task-branch delete sub-step receives git's "remote ref does not exist" or "unable to delete ... not found" response
- **THEN** iris treats the step as successful and appends the `remote_task_branch_deleted` checkpoint

#### Scenario: Invalid merge strategy rejected upfront

- **WHEN** `merge_strategy` is neither `"no_ff"` nor `"ff_only"`
- **THEN** iris returns a structured error naming the invalid value and performs no work

#### Scenario: Default-branch push refused by `iris:push` runs inline here

- **GIVEN** `iris:push` refuses to push the default branch as a safety guard
- **WHEN** `iris:complete_task` reaches the default-branch-push sub-step
- **THEN** iris runs `git push origin <default>` directly under the same per-source-repo mutex, bypassing the `iris:push` guard because this verb's contract is specifically to ship the default branch after merging into it

#### Scenario: Three git sub-steps share one lock acquisition

- **WHEN** `iris:complete_task` runs the merge, default-branch push, and remote-branch delete
- **THEN** all three sub-steps execute under a single `lockSourceRepo` acquisition; no other iris verb against the same source repo can interleave between them

#### Scenario: Direct CLI invocation runs the same verb

- **WHEN** the user runs `iris complete-task <task-id> [--merge-strategy no_ff|ff_only]`
- **THEN** the same `verbs.CompleteTask` Go function executes (bypassing the daemon process) and prints the structured result

