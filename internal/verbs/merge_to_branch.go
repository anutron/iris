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

// MergeToBranchResult is the structured success payload for
// iris:merge_to_branch.
type MergeToBranchResult struct {
	SHA          string `json:"sha"`
	TargetBranch string `json:"target_branch"`
	SourceRef    string `json:"source_ref"`
	SourceRepo   string `json:"source_repo"`
	// Pushed is true once target_branch has been pushed to origin. Always
	// false on a dry run.
	Pushed bool   `json:"pushed"`
	Log    string `json:"log"`

	// PostMerge is the outcome of the target branch's `.iris.toml`
	// [post_merge] hook. Null when no hook is configured or when DryRun is
	// true.
	PostMerge *PostMergeResult `json:"post_merge,omitempty"`

	// Dry-run-only fields. Zero-valued on a real merge.
	DryRun       bool     `json:"dry_run"`
	WouldSucceed bool     `json:"would_succeed,omitempty"`
	FilesChanged []string `json:"files_changed,omitempty"`
	Conflicts    []string `json:"conflicts,omitempty"`
}

// MergeToBranch resolves the task's source repo, merges source_ref into
// target_branch, and pushes target_branch to origin. Unlike MergeToMaster,
// the merge happens in a scratch `git worktree add` (a temporary directory
// removed on exit) so the source repo's currently checked-out branch and
// working tree are never touched. target_branch and source_ref may be
// arbitrary — neither is constrained to any branch-naming prefix.
func MergeToBranch(ctx context.Context, client *argus.Client, taskID, targetBranch, sourceRef string, opts MergeOptions) (*MergeToBranchResult, error) {
	if err := validateMergeToBranchArgs(targetBranch, sourceRef); err != nil {
		return nil, err
	}

	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	defaultBranch, err := DefaultBranch(ctx, resolved.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("determine default branch: %w", err)
	}

	if targetBranch == defaultBranch || targetBranch == "main" || targetBranch == "master" {
		return nil, fmt.Errorf("refusing to target the default/protected branch %q; use iris_merge_to_master instead", targetBranch)
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	if opts.DryRun {
		return dryRunMergeToBranchLocked(ctx, resolved, targetBranch, sourceRef)
	}
	return mergeToBranchLocked(ctx, resolved, targetBranch, sourceRef, opts)
}

// validateMergeToBranchArgs checks target_branch/source_ref in isolation,
// before any argus lookup or git invocation: both required, neither may
// begin with '-' (the same flag-smuggling guard used for head/branch/remote/
// base_repo elsewhere in the verb set), and a branch must not be merged into
// itself. The default/protected-branch guard lives in MergeToBranch itself
// because it needs the resolved default branch.
func validateMergeToBranchArgs(targetBranch, sourceRef string) error {
	if targetBranch == "" {
		return fmt.Errorf("target_branch is required")
	}
	if sourceRef == "" {
		return fmt.Errorf("source_ref is required")
	}
	if strings.HasPrefix(targetBranch, "-") {
		return fmt.Errorf("invalid target_branch %q (must not begin with '-')", targetBranch)
	}
	if strings.HasPrefix(sourceRef, "-") {
		return fmt.Errorf("invalid source_ref %q (must not begin with '-')", sourceRef)
	}
	if targetBranch == sourceRef {
		return fmt.Errorf("refusing to merge %q into itself", targetBranch)
	}
	return nil
}

// setupScratchWorktree fetches the source repo, creates a scratch `git
// worktree add` checked out at targetBranch in a fresh temp directory, and
// reconciles it against origin/<targetBranch> when that ref exists (so a
// local ref that is stale or doesn't track origin never produces a merge
// based on out-of-date state). It returns the worktree path and a cleanup
// closure the caller MUST defer immediately.
//
// git worktree add's own DWIM behavior creates a local tracking branch from
// origin/<targetBranch> when no local branch exists; when a local branch
// DOES exist (whether or not it tracks origin), worktree add uses it as-is,
// which is exactly why the explicit reset-to-origin step below is needed.
func setupScratchWorktree(ctx context.Context, sourceRepo, targetBranch string) (string, func(), error) {
	if out, err := runGit(ctx, sourceRepo, "fetch", "--all", "--prune"); err != nil {
		return "", nil, fmt.Errorf("fetch: %w; log:\n%s", err, out)
	}

	tempDir, err := os.MkdirTemp("", "iris-merge-to-branch-*")
	if err != nil {
		return "", nil, fmt.Errorf("create scratch worktree dir: %w", err)
	}

	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if out, err := runGit(cleanupCtx, sourceRepo, "worktree", "remove", "--force", tempDir); err != nil {
			slog.Warn("scratch worktree remove failed",
				"path", tempDir,
				"err", err,
				"output", strings.TrimSpace(out),
			)
			_ = os.RemoveAll(tempDir)
			if _, pruneErr := runGit(cleanupCtx, sourceRepo, "worktree", "prune"); pruneErr != nil {
				slog.Warn("scratch worktree prune failed", "err", pruneErr)
			}
		}
	}

	if out, err := runGit(ctx, sourceRepo, "worktree", "add", tempDir, targetBranch); err != nil {
		_ = os.RemoveAll(tempDir)
		return "", nil, fmt.Errorf("checkout target_branch %s in scratch worktree: %w; log:\n%s", targetBranch, err, out)
	}

	if _, err := runGit(ctx, tempDir, "rev-parse", "--verify", "--quiet", "origin/"+targetBranch); err == nil {
		if out, err := runGit(ctx, tempDir, "reset", "--hard", "origin/"+targetBranch); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("reset scratch worktree to origin/%s: %w; log:\n%s", targetBranch, err, out)
		}
	}

	return tempDir, cleanup, nil
}

// mergeToBranchLocked performs the merge + push sequence assuming the
// caller already holds lockSourceRepo(resolved.SourceRepo).
func mergeToBranchLocked(ctx context.Context, resolved *ResolvedRepo, targetBranch, sourceRef string, opts MergeOptions) (*MergeToBranchResult, error) {
	worktreePath, cleanup, err := setupScratchWorktree(ctx, resolved.SourceRepo, targetBranch)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Deferred cleanup: if the caller's context is cancelled mid-merge, the
	// git process gets killed and MERGE_HEAD may persist in the scratch
	// worktree. Best-effort abort with a fresh short context, mirroring
	// mergeToMasterLocked's equivalent guard.
	mergeStarted := false
	defer func() {
		if !mergeStarted || ctx.Err() == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := runGit(cleanupCtx, worktreePath, "merge", "--abort"); err != nil {
			slog.Warn("deferred merge --abort failed after ctx cancel",
				"worktree", worktreePath,
				"err", err,
				"output", strings.TrimSpace(out),
			)
		}
	}()

	var logBuf strings.Builder

	mergeArgs := []string{"merge"}
	if opts.NoFF {
		mergeArgs = append(mergeArgs, "--no-ff")
	} else {
		mergeArgs = append(mergeArgs, "--ff-only")
	}
	if opts.Message != "" {
		mergeArgs = append(mergeArgs, "-m", opts.Message)
	}
	mergeArgs = append(mergeArgs, sourceRef)

	mergeStarted = true
	if out, err := runGit(ctx, worktreePath, mergeArgs...); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("merge cancelled: %w", ctx.Err())
		}
		logBuf.WriteString(out)
		if _, abortErr := runGit(ctx, worktreePath, "merge", "--abort"); abortErr != nil {
			return nil, fmt.Errorf("merge failed and abort failed: %w (abort: %v); log:\n%s",
				err, abortErr, logBuf.String())
		}
		return nil, fmt.Errorf("merge %s into %s failed (aborted): %w; log:\n%s",
			sourceRef, targetBranch, err, logBuf.String())
	} else {
		logBuf.WriteString(out)
	}
	mergeStarted = false // merge completed cleanly; deferred cleanup is a no-op

	sha, err := runGit(ctx, worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read merge sha: %w", err)
	}
	mergeSHA := strings.TrimSpace(sha)

	if out, err := runGit(ctx, worktreePath, "push", "origin", targetBranch); err != nil {
		return nil, fmt.Errorf("push %s to origin: %w; log:\n%s", targetBranch, err, out)
	} else {
		logBuf.WriteString(out)
	}

	result := &MergeToBranchResult{
		SHA:          mergeSHA,
		TargetBranch: targetBranch,
		SourceRef:    sourceRef,
		SourceRepo:   resolved.SourceRepo,
		Pushed:       true,
		Log:          logBuf.String(),
	}

	postMerge, err := runMergeToBranchPostMergeHook(ctx, resolved, worktreePath, targetBranch, sourceRef, mergeSHA)
	if err != nil {
		slog.Warn("post_merge hook skipped: could not load .iris.toml",
			"source_repo", resolved.SourceRepo,
			"target_branch", targetBranch,
			"err", err,
		)
	}
	result.PostMerge = postMerge

	return result, nil
}

// dryRunMergeToBranchLocked previews the merge in the scratch worktree with
// `--no-commit --no-ff`, capturing the would-be state before aborting
// unconditionally. No push, no post_merge hook. Mirrors dryRunMergeLocked's
// shape.
func dryRunMergeToBranchLocked(ctx context.Context, resolved *ResolvedRepo, targetBranch, sourceRef string) (*MergeToBranchResult, error) {
	worktreePath, cleanup, err := setupScratchWorktree(ctx, resolved.SourceRepo, targetBranch)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	mergeStarted := false
	defer func() {
		if !mergeStarted {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := runGit(cleanupCtx, worktreePath, "merge", "--abort"); err != nil {
			slog.Warn("dry-run deferred merge --abort failed",
				"worktree", worktreePath,
				"err", err,
				"output", strings.TrimSpace(out),
			)
		}
	}()

	var logBuf strings.Builder

	mergeStarted = true
	mergeOut, mergeErr := runGit(ctx, worktreePath, "merge", "--no-commit", "--no-ff", sourceRef)
	logBuf.WriteString(mergeOut)
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("dry-run merge cancelled: %w", ctx.Err())
	}

	filesChanged := listGitPaths(ctx, worktreePath, "diff", "--cached", "--name-only")
	conflicts := listGitPaths(ctx, worktreePath, "diff", "--name-only", "--diff-filter=U")

	if _, abortErr := runGit(ctx, worktreePath, "merge", "--abort"); abortErr != nil {
		return nil, fmt.Errorf("dry-run abort failed: %w (merge output:\n%s)", abortErr, mergeOut)
	}
	mergeStarted = false

	wouldSucceed := mergeErr == nil && len(conflicts) == 0
	return &MergeToBranchResult{
		TargetBranch: targetBranch,
		SourceRef:    sourceRef,
		SourceRepo:   resolved.SourceRepo,
		Log:          logBuf.String(),
		DryRun:       true,
		WouldSucceed: wouldSucceed,
		FilesChanged: filesChanged,
		Conflicts:    conflicts,
	}, nil
}

// runMergeToBranchPostMergeHook loads .iris.toml from worktreePath — the
// scratch worktree's tree, now at the merge commit, i.e. target_branch's
// tree — NOT resolved.SourceRepo, which merge_to_branch never disturbs and
// whose current checkout may be an unrelated branch. Runs the [post_merge]
// hook if declared, mirroring runPostMergeHook's exec/timeout/capture shape
// with env vars specific to this verb (IRIS_TARGET_BRANCH/IRIS_SOURCE_REF in
// place of IRIS_DEFAULT_BRANCH/IRIS_TASK_BRANCH).
func runMergeToBranchPostMergeHook(ctx context.Context, resolved *ResolvedRepo, worktreePath, targetBranch, sourceRef, mergeSHA string) (*PostMergeResult, error) {
	configPath := filepath.Join(worktreePath, config.IrisTomlFilename)
	doc, _, err := config.LoadIrisToml(configPath, false)
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.PostMerge == nil {
		return nil, nil
	}

	hook := doc.PostMerge
	if len(hook.Command) == 0 {
		return nil, nil
	}

	timeoutSec := hook.ResolvedTimeoutSeconds(config.DefaultHookTimeoutSeconds)
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, hook.Command[0], hook.Command[1:]...)
	cmd.Dir = worktreePath
	if hook.WorkingDirectory != "" && hook.WorkingDirectory != "." {
		cmd.Dir = filepath.Join(worktreePath, hook.WorkingDirectory)
	}
	cmd.Env = append(os.Environ(),
		"IRIS_TASK_ID="+resolved.Task.ID,
		"IRIS_SOURCE_REPO="+resolved.SourceRepo,
		"IRIS_TARGET_BRANCH="+targetBranch,
		"IRIS_SOURCE_REF="+sourceRef,
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
		result.ExitCode = -1
		result.Error = runErr.Error()
		return result, nil
	}

	result.ExitCode = 0
	return result, nil
}
