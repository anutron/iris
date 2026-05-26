// Package verbs implements iris's host-side tool surface. Each verb is a
// Go function with named, typed arguments. The CLI and the MCP handler
// both call the same function; there is no shared mutable state besides
// the per-source-repo mutex map.
package verbs

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// ResolvedRepo is everything a verb needs to safely operate on a
// source repo on behalf of an argus task.
type ResolvedRepo struct {
	Task         *argus.Task
	WorktreePath string // absolute path to the argus worktree
	SourceRepo   string // absolute path to the canonical source repo's working tree
	Branch       string // current branch of the worktree (e.g., "argus/handoff-...")
}

// Resolve looks up the argus task by ID, then derives the canonical
// source-repo path from the worktree via `git rev-parse --git-common-dir`.
// Verbs MUST call this — they MUST NOT accept agent-supplied filesystem
// paths.
func Resolve(ctx context.Context, client *argus.Client, taskID string) (*ResolvedRepo, error) {
	task, err := client.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("argus task %q: %w", taskID, err)
	}
	if task.WorktreePath == "" {
		return nil, fmt.Errorf("argus task %q has no worktree_path", taskID)
	}

	srcRepo, err := sourceRepoFromWorktree(ctx, task.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("derive source repo from %s: %w", task.WorktreePath, err)
	}

	branch, err := currentBranch(ctx, task.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("read current branch of %s: %w", task.WorktreePath, err)
	}

	return &ResolvedRepo{
		Task:         task,
		WorktreePath: task.WorktreePath,
		SourceRepo:   srcRepo,
		Branch:       branch,
	}, nil
}

// sourceRepoFromWorktree runs `git -C <worktree> rev-parse --git-common-dir`
// which returns the canonical .git directory (e.g.,
// `/Users/aaron/Development/Personal/iris/.git`). The source repo's
// working tree is the parent of that .git directory.
//
// The returned path has symlinks resolved (filepath.EvalSymlinks) so it
// matches what `git -C <path>` returns from elsewhere — important on
// macOS where /var is a symlink to /private/var, and important for
// project-allowlist comparison against argus's project list.
func sourceRepoFromWorktree(ctx context.Context, worktreePath string) (string, error) {
	out, err := runGit(ctx, worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	gitDir := strings.TrimSpace(out)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}
	gitDir, err = filepath.Abs(gitDir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(gitDir)
	if err == nil {
		gitDir = resolved
	}
	return filepath.Dir(gitDir), nil
}

func currentBranch(ctx context.Context, worktreePath string) (string, error) {
	out, err := runGit(ctx, worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// DefaultBranch returns "main" or "master" by reading
// `git symbolic-ref refs/remotes/origin/HEAD` in the source repo.
// Falls back to "main" if the symbolic ref is not set.
func DefaultBranch(ctx context.Context, sourceRepo string) (string, error) {
	out, err := runGit(ctx, sourceRepo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		// origin/HEAD isn't always set; default to "main".
		return "main", nil
	}
	ref := strings.TrimSpace(out) // e.g., "origin/main"
	if i := strings.IndexByte(ref, '/'); i >= 0 {
		return ref[i+1:], nil
	}
	return ref, nil
}

// runGit runs `git -C <dir> <args>` and returns combined stdout+stderr.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(full, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
