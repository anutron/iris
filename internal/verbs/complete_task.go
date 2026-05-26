package verbs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// Checkpoint names for the CompleteTask flow. Returned in CompleteTaskResult
// so a partial failure tells the caller where to resume.
const (
	CheckpointMerged                = "merged"
	CheckpointDefaultBranchPushed   = "default_branch_pushed"
	CheckpointRemoteTaskBranchDeleted = "remote_task_branch_deleted"
	CheckpointTaskMarkedComplete    = "task_marked_complete"
	CheckpointTaskArchived          = "task_archived"
)

// CompleteTaskOptions captures the merge strategy for the embedded
// MergeToMaster step.
type CompleteTaskOptions struct {
	// MergeStrategy is "no_ff" (default) or "ff_only". Anything else is rejected.
	MergeStrategy string
}

// CompleteTaskResult lists the checkpoints reached and the wrapped error
// (empty on full success). Caller re-invokes the verb to resume on partial
// success; each sub-step is idempotent.
type CompleteTaskResult struct {
	Checkpoints []string `json:"checkpoints"`
	Error       string   `json:"error,omitempty"`
}

// CompleteTask runs the full ship-it sequence: merge -> push default ->
// delete remote task branch -> mark argus task complete -> archive.
//
// Idempotency: if the task is already "complete" on argus when the verb is
// invoked, returns success with all checkpoints (no work performed).
// Each sub-step is structured so a retry after partial failure is safe:
// remote-branch-delete tolerates "already gone"; the status POST is
// idempotent; archive is best-effort and skipped if it fails.
func CompleteTask(ctx context.Context, client *argus.Client, taskID string, opts CompleteTaskOptions) (*CompleteTaskResult, error) {
	strategy := opts.MergeStrategy
	if strategy == "" {
		strategy = "no_ff"
	}
	if strategy != "no_ff" && strategy != "ff_only" {
		return nil, fmt.Errorf("invalid merge_strategy %q: must be \"no_ff\" or \"ff_only\"", strategy)
	}

	task, err := client.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("argus task %q: %w", taskID, err)
	}
	if task.Status == "complete" {
		return &CompleteTaskResult{
			Checkpoints: []string{
				CheckpointMerged,
				CheckpointDefaultBranchPushed,
				CheckpointRemoteTaskBranchDeleted,
				CheckpointTaskMarkedComplete,
				CheckpointTaskArchived,
			},
		}, nil
	}

	result := &CompleteTaskResult{}

	mergeOpts := MergeOptions{NoFF: strategy == "no_ff"}
	mergeResult, err := MergeToMaster(ctx, client, taskID, mergeOpts)
	if err != nil {
		return result, fmt.Errorf("merge_to_master: %w", err)
	}
	result.Checkpoints = append(result.Checkpoints, CheckpointMerged)

	// Push the default branch (mergeResult.DefaultBranch). The Push verb
	// refuses default-branch pushes for safety, so we call git directly
	// here under the same per-source-repo mutex.
	if err := pushDefaultBranch(ctx, mergeResult.SourceRepo, mergeResult.DefaultBranch); err != nil {
		return result, fmt.Errorf("push default branch %s: %w", mergeResult.DefaultBranch, err)
	}
	result.Checkpoints = append(result.Checkpoints, CheckpointDefaultBranchPushed)

	if err := deleteRemoteBranch(ctx, mergeResult.SourceRepo, mergeResult.TaskBranch); err != nil {
		return result, fmt.Errorf("delete remote branch %s: %w", mergeResult.TaskBranch, err)
	}
	result.Checkpoints = append(result.Checkpoints, CheckpointRemoteTaskBranchDeleted)

	if err := client.SetTaskStatus(ctx, taskID, "complete"); err != nil {
		return result, fmt.Errorf("mark task complete: %w", err)
	}
	result.Checkpoints = append(result.Checkpoints, CheckpointTaskMarkedComplete)

	// Archive is best-effort. Argus archives the task AND deletes the
	// worktree; a failure here leaves the task in "complete" status with
	// the worktree intact — operator can clean up manually. We log via the
	// returned error string but still treat the overall flow as success.
	if err := client.ArchiveTask(ctx, taskID); err != nil {
		result.Error = fmt.Sprintf("archive (best-effort): %v", err)
		return result, nil
	}
	result.Checkpoints = append(result.Checkpoints, CheckpointTaskArchived)

	return result, nil
}

// pushDefaultBranch pushes the source repo's currently-checked-out default
// branch to origin. MergeToMaster left the source repo checked out on the
// default branch with the new merge commit, so this is just `git push origin
// <default>`.
func pushDefaultBranch(ctx context.Context, sourceRepo, defaultBranch string) error {
	mu := lockSourceRepo(sourceRepo)
	defer mu.Unlock()
	if out, err := runGit(ctx, sourceRepo, "push", "origin", defaultBranch); err != nil {
		return fmt.Errorf("git push origin %s: %w; log:\n%s", defaultBranch, err, out)
	}
	return nil
}

// deleteRemoteBranch issues `git push origin --delete <branch>`. A
// "branch not found" response from origin is treated as success — the goal
// is that the remote branch is absent, not that we performed the deletion.
func deleteRemoteBranch(ctx context.Context, sourceRepo, branch string) error {
	mu := lockSourceRepo(sourceRepo)
	defer mu.Unlock()
	out, err := runGit(ctx, sourceRepo, "push", "origin", "--delete", branch)
	if err == nil {
		return nil
	}
	if branchAlreadyDeleted(out) {
		return nil
	}
	return errors.New(strings.TrimSpace(out))
}

// branchAlreadyDeleted recognises git's "remote ref does not exist" output
// so a retried CompleteTask after the branch was already removed succeeds.
func branchAlreadyDeleted(gitOutput string) bool {
	out := strings.ToLower(gitOutput)
	return strings.Contains(out, "remote ref does not exist") ||
		strings.Contains(out, "unable to delete") && strings.Contains(out, "not found")
}
