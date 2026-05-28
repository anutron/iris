package verbs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// MergeOptions captures the per-call knobs for MergeToMaster.
type MergeOptions struct {
	// NoFF emits `git merge --no-ff` when true (default), `--ff-only`
	// when false.
	NoFF bool

	// Message, if non-empty, is passed as `-m <message>` to git merge.
	Message string

	// DryRun runs `git merge --no-commit --no-ff <branch>`, captures
	// the would-be merge state, aborts cleanly, and returns a preview
	// result. No commit, no post_merge hook.
	DryRun bool
}

// PostMergeResult is the captured outcome of the `[post_merge]` hook
// declared in `.iris.toml`. Populated only after a successful (non-dry)
// merge when the config provides the hook. Iris does NOT roll back the
// merge on a non-zero exit_code; the hook is informational.
type PostMergeResult struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
	// Error is non-empty when iris could not execute the hook to
	// completion (timeout, binary missing). For a hook that ran but
	// exited non-zero, Error is empty and ExitCode reflects the exit.
	Error string `json:"error,omitempty"`
}

// MergeResult is the structured success payload.
type MergeResult struct {
	SHA           string `json:"sha"`
	DefaultBranch string `json:"default_branch"`
	TaskBranch    string `json:"task_branch"`
	SourceRepo    string `json:"source_repo"`
	Log           string `json:"log"`

	// Postconditions describe iris's output state. They are facts, not
	// directives. Always true today; named so consumers can read them
	// as "merge does not clean up."
	TaskBranchStillExists bool `json:"task_branch_still_exists"`
	WorktreeStillPresent  bool `json:"worktree_still_present"`

	// PostMerge is the outcome of the `.iris.toml` `[post_merge]` hook.
	// Null when no hook is configured or when DryRun is true.
	PostMerge *PostMergeResult `json:"post_merge,omitempty"`

	// Dry-run-only fields. Zero-valued on a real merge.
	DryRun       bool     `json:"dry_run"`
	WouldSucceed bool     `json:"would_succeed,omitempty"`
	FilesChanged []string `json:"files_changed,omitempty"`
	Conflicts    []string `json:"conflicts,omitempty"`
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

	if opts.DryRun {
		return dryRunMergeLocked(ctx, resolved, defaultBranch)
	}

	return mergeToMasterLocked(ctx, resolved, defaultBranch, opts)
}

// mergeToMasterLocked performs the merge sequence assuming the caller
// already holds lockSourceRepo(resolved.SourceRepo). It exists so composite
// verbs (e.g. CompleteTask) can hold a single lock across merge -> push
// default -> delete remote branch without the sub-step releasing between
// each git op.
func mergeToMasterLocked(ctx context.Context, resolved *ResolvedRepo, defaultBranch string, opts MergeOptions) (*MergeResult, error) {
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
		if out, err := runGit(cleanup, resolved.SourceRepo, "merge", "--abort"); err != nil {
			// Cleanup is best-effort; log at Warn so a broken cleanup is
			// visible rather than silently leaving MERGE_HEAD behind.
			slog.Warn("deferred merge --abort failed after ctx cancel",
				"source_repo", resolved.SourceRepo,
				"err", err,
				"output", strings.TrimSpace(out),
			)
		}
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
	mergeSHA := strings.TrimSpace(sha)

	result := &MergeResult{
		SHA:                   mergeSHA,
		DefaultBranch:         defaultBranch,
		TaskBranch:            resolved.Branch,
		SourceRepo:            resolved.SourceRepo,
		Log:                   logBuf.String(),
		TaskBranchStillExists: true,
		WorktreeStillPresent:  true,
	}

	postMerge, err := runPostMergeHook(ctx, resolved, defaultBranch, mergeSHA)
	if err != nil {
		// Loading the config failed in a way that wasn't "file missing"
		// (a parse error or I/O failure). Surface so the operator can
		// fix the toml; merge already succeeded so we don't roll back.
		slog.Warn("post_merge hook skipped: could not load .iris.toml",
			"source_repo", resolved.SourceRepo,
			"err", err,
		)
	}
	result.PostMerge = postMerge

	return result, nil
}

// dryRunMergeLocked runs the real merge sequence with `--no-commit
// --no-ff`, captures the would-be state, then aborts unconditionally so
// the source repo lands back on the default branch.
//
// Per spec: holds the same per-source-repo lock as a real merge, runs
// fetch + checkout + pull --ff-only first (so the dry-run is against
// the up-to-date default branch), captures files_changed and conflicts
// BEFORE merge --abort wipes the index state.
func dryRunMergeLocked(ctx context.Context, resolved *ResolvedRepo, defaultBranch string) (*MergeResult, error) {
	// Always abort whatever in-progress merge we may have started, even
	// on ctx cancel mid-flight. Mirrors the real merge path's deferred
	// cleanup so MERGE_HEAD never leaks.
	mergeStarted := false
	defer func() {
		if !mergeStarted {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := runGit(cleanup, resolved.SourceRepo, "merge", "--abort"); err != nil {
			slog.Warn("dry-run deferred merge --abort failed",
				"source_repo", resolved.SourceRepo,
				"err", err,
				"output", strings.TrimSpace(out),
			)
		}
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

	mergeStarted = true
	mergeOut, mergeErr := runGit(ctx, resolved.SourceRepo, "merge", "--no-commit", "--no-ff", resolved.Branch)
	logBuf.WriteString(mergeOut)
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("dry-run merge cancelled: %w", ctx.Err())
	}

	// Capture state BEFORE the abort wipes the index. `git diff --cached`
	// lists files staged by the merge attempt; --diff-filter=U lists
	// unmerged (conflicted) paths.
	filesChanged := listGitPaths(ctx, resolved.SourceRepo, "diff", "--cached", "--name-only")
	conflicts := listGitPaths(ctx, resolved.SourceRepo, "diff", "--name-only", "--diff-filter=U")

	if _, abortErr := runGit(ctx, resolved.SourceRepo, "merge", "--abort"); abortErr != nil {
		// The merge --abort failed; that's a real problem because the
		// repo is now in an unknown state. Surface up so the operator
		// can recover.
		return nil, fmt.Errorf("dry-run abort failed: %w (merge output:\n%s)", abortErr, mergeOut)
	}
	mergeStarted = false

	wouldSucceed := mergeErr == nil && len(conflicts) == 0
	return &MergeResult{
		DefaultBranch:         defaultBranch,
		TaskBranch:            resolved.Branch,
		SourceRepo:            resolved.SourceRepo,
		Log:                   logBuf.String(),
		TaskBranchStillExists: true,
		WorktreeStillPresent:  true,
		DryRun:                true,
		WouldSucceed:          wouldSucceed,
		FilesChanged:          filesChanged,
		Conflicts:             conflicts,
	}, nil
}

// listGitPaths runs `git <args>` and returns the trimmed output split on
// newlines, with empty entries dropped. Best-effort: on error returns
// nil rather than failing the caller.
func listGitPaths(ctx context.Context, dir string, args ...string) []string {
	out, err := runGit(ctx, dir, args...)
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

// runPostMergeHook loads .iris.toml from the source repo and runs the
// `[post_merge]` hook if declared. Returns (nil, nil) when no .iris.toml
// is present or no hook is configured. A non-zero exit_code or an exec
// failure is captured into the result, NOT propagated as an error: the
// merge already succeeded and the hook outcome is informational.
//
// The returned error is reserved for "the config itself was unusable"
// (parse error, I/O failure). Callers may log it but should still return
// success since the merge committed.
func runPostMergeHook(ctx context.Context, resolved *ResolvedRepo, defaultBranch, mergeSHA string) (*PostMergeResult, error) {
	configPath := filepath.Join(resolved.SourceRepo, config.IrisTomlFilename)
	// isSelf=false: post_merge does not interact with the exit_code
	// self-only mechanism; cross-target validation is sufficient here.
	doc, _, err := config.LoadIrisToml(configPath, false)
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.PostMerge == nil {
		return nil, nil
	}

	hook := doc.PostMerge
	if len(hook.Command) == 0 {
		// Validation should have caught this, but defend against
		// a caller that hand-built a malformed IrisToml.
		return nil, nil
	}

	timeoutSec := hook.ResolvedTimeoutSeconds(config.DefaultHookTimeoutSeconds)
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, hook.Command[0], hook.Command[1:]...)
	cmd.Dir = resolved.SourceRepo
	if hook.WorkingDirectory != "" && hook.WorkingDirectory != "." {
		cmd.Dir = filepath.Join(resolved.SourceRepo, hook.WorkingDirectory)
	}
	cmd.Env = append(os.Environ(),
		"IRIS_TASK_ID="+resolved.Task.ID,
		"IRIS_TASK_BRANCH="+resolved.Branch,
		"IRIS_SOURCE_REPO="+resolved.SourceRepo,
		"IRIS_DEFAULT_BRANCH="+defaultBranch,
		"IRIS_MERGE_SHA="+mergeSHA,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	dur := time.Since(start)

	result := &PostMergeResult{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMs: dur.Milliseconds(),
	}

	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		result.Error = fmt.Sprintf("timeout after %ds", timeoutSec)
		return result, nil
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		// Couldn't start the process (binary missing, perms, etc.).
		result.ExitCode = -1
		result.Error = runErr.Error()
		return result, nil
	}

	result.ExitCode = 0
	return result, nil
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
