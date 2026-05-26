package verbs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// MergeOptions captures the per-call knobs for MergeToMaster.
type MergeOptions struct {
	// NoFF emits `git merge --no-ff` when true (default), `--ff-only`
	// when false.
	NoFF bool

	// Message, if non-empty, is passed as `-m <message>` to git merge.
	Message string
}

// MergeResult is the structured success payload.
type MergeResult struct {
	SHA           string `json:"sha"`
	DefaultBranch string `json:"default_branch"`
	TaskBranch    string `json:"task_branch"`
	SourceRepo    string `json:"source_repo"`
	Log           string `json:"log"`
}

// MergeToMaster resolves the task's source repo and merges the task's
// branch into master under the per-source-repo mutex.
//
// Safety: refuses any branch that is not prefixed `argus/`. Refuses to
// merge the default branch into itself. Aborts cleanly on conflict.
func MergeToMaster(ctx context.Context, client *argus.Client, taskID string, opts MergeOptions) (*MergeResult, error) {
	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	defaultBranch, err := DefaultBranch(ctx, resolved.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("determine default branch: %w", err)
	}

	if err := guardBranch(resolved.Branch, defaultBranch); err != nil {
		return nil, err
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	// Deferred cleanup: if the caller's context is cancelled mid-merge,
	// the git process gets killed and MERGE_HEAD may persist. Best-effort
	// abort with a fresh short context so the source repo lands clean
	// even when the caller's deadline already elapsed.
	mergeStarted := false
	defer func() {
		if !mergeStarted || ctx.Err() == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = runGit(cleanup, resolved.SourceRepo, "merge", "--abort")
		cancel()
	}()

	var logBuf strings.Builder

	if out, err := runGit(ctx, resolved.SourceRepo, "fetch", "--all", "--prune"); err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	} else {
		logBuf.WriteString(out)
	}

	if out, err := runGit(ctx, resolved.SourceRepo, "checkout", defaultBranch); err != nil {
		return nil, fmt.Errorf("checkout %s: %w", defaultBranch, err)
	} else {
		logBuf.WriteString(out)
	}

	if out, err := runGit(ctx, resolved.SourceRepo, "pull", "--ff-only"); err != nil {
		return nil, fmt.Errorf("pull --ff-only %s: %w", defaultBranch, err)
	} else {
		logBuf.WriteString(out)
	}

	mergeArgs := []string{"merge"}
	if opts.NoFF {
		mergeArgs = append(mergeArgs, "--no-ff")
	} else {
		mergeArgs = append(mergeArgs, "--ff-only")
	}
	if opts.Message != "" {
		mergeArgs = append(mergeArgs, "-m", opts.Message)
	}
	mergeArgs = append(mergeArgs, resolved.Branch)

	mergeStarted = true
	if out, err := runGit(ctx, resolved.SourceRepo, mergeArgs...); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("merge cancelled: %w", ctx.Err())
		}
		logBuf.WriteString(out)
		if _, abortErr := runGit(ctx, resolved.SourceRepo, "merge", "--abort"); abortErr != nil {
			return nil, fmt.Errorf("merge failed and abort failed: %w (abort: %v); log:\n%s",
				err, abortErr, logBuf.String())
		}
		return nil, fmt.Errorf("merge %s into %s failed (aborted): %w; log:\n%s",
			resolved.Branch, defaultBranch, err, logBuf.String())
	} else {
		logBuf.WriteString(out)
	}
	mergeStarted = false // merge completed cleanly; deferred cleanup is a no-op

	sha, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read merge sha: %w", err)
	}

	return &MergeResult{
		SHA:           strings.TrimSpace(sha),
		DefaultBranch: defaultBranch,
		TaskBranch:    resolved.Branch,
		SourceRepo:    resolved.SourceRepo,
		Log:           logBuf.String(),
	}, nil
}

// guardBranch enforces the branch-scope safety contract.
func guardBranch(branch, defaultBranch string) error {
	if branch == "" {
		return fmt.Errorf("task has no current branch")
	}
	if branch == defaultBranch {
		return fmt.Errorf("refusing to merge %s into %s (same branch)", branch, defaultBranch)
	}
	if branch == "main" || branch == "master" {
		return fmt.Errorf("refusing to merge protected branch %q into %s", branch, defaultBranch)
	}
	if !strings.HasPrefix(branch, "argus/") {
		return fmt.Errorf("refusing to merge non-argus branch %q (must start with 'argus/')", branch)
	}
	return nil
}
