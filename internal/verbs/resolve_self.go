package verbs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// executable indirection lets tests pretend iris is running from a
// specific binary path without actually moving the test binary around.
var executable = os.Executable

// ResolveSelf discovers iris's own source repo from the running binary's
// path. It calls os.Executable(), follows symlinks via filepath.EvalSymlinks,
// then walks up to find the nearest `.git` directory.
//
// The returned ResolvedRepo has Task=nil (no argus task exists for a
// self-target), and WorktreePath == SourceRepo (the source repo IS the
// working location).
func ResolveSelf(ctx context.Context) (*ResolvedRepo, error) {
	exe, err := executable()
	if err != nil {
		return nil, fmt.Errorf("resolve self: os.Executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// EvalSymlinks fails when the binary path no longer exists; fall
		// back to the raw path so callers can still get an error citing it.
		resolved = exe
	}
	root, err := walkToGitRoot(resolved)
	if err != nil {
		return nil, fmt.Errorf("resolve self from %s: %w", resolved, err)
	}
	canonical := canonicalize(root)
	return &ResolvedRepo{
		Task:         nil,
		WorktreePath: canonical,
		SourceRepo:   canonical,
		Branch:       "",
	}, nil
}

// ResolvePath resolves a filesystem path to a source repo via git's own
// `rev-parse --git-common-dir`. The path may be relative or absolute,
// and may use `~` (expanded against the current user's home).
//
// The argus project allowlist is enforced just like Resolve(). Self-hosting
// verbs decide whether to call this or ResolveSelf depending on context.
func ResolvePath(ctx context.Context, client *argus.Client, path string) (*ResolvedRepo, error) {
	if path == "" {
		return nil, fmt.Errorf("resolve path: empty path")
	}
	expanded, err := expandTilde(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path %s: %w", path, err)
	}
	if _, err := os.Stat(expanded); err != nil {
		return nil, fmt.Errorf("resolve path %s: %w", path, err)
	}
	srcRepo, err := sourceRepoFromWorktree(ctx, expanded)
	if err != nil {
		return nil, fmt.Errorf("resolve path %s: %w", path, err)
	}
	if err := assertAllowlisted(ctx, client, srcRepo); err != nil {
		return nil, err
	}
	canonical := canonicalize(srcRepo)
	return &ResolvedRepo{
		Task:         nil,
		WorktreePath: canonical,
		SourceRepo:   canonical,
		Branch:       "",
	}, nil
}

// ResolveTarget is the dispatcher used by self-hosting verbs (reload,
// validate_config, status). The (taskID, path) pair are mutually
// exclusive; both supplied is ambiguous and rejected. Neither supplied
// resolves to iris itself via ResolveSelf.
//
// Allowlist enforcement is conditional:
//   - task_id resolves via Resolve() which enforces the allowlist.
//   - path resolves via ResolvePath() which enforces the allowlist.
//   - no inputs resolves via ResolveSelf() which does NOT enforce the
//     allowlist (iris's own repo is implicit; the self-hosting verb is
//     the constraint, not the allowlist).
func ResolveTarget(ctx context.Context, client *argus.Client, taskID, path string) (*ResolvedRepo, error) {
	switch {
	case taskID != "" && path != "":
		return nil, fmt.Errorf("ambiguous: pass either task_id or path, not both")
	case taskID != "":
		return Resolve(ctx, client, taskID)
	case path != "":
		return ResolvePath(ctx, client, path)
	default:
		return ResolveSelf(ctx)
	}
}

// walkToGitRoot ascends from start (a file or directory) to the nearest
// ancestor that contains a `.git` entry. Returns the ancestor path.
//
// The .git entry may be a directory (normal repo, primary worktree) or a
// file (linked worktree containing `gitdir: ...`).
func walkToGitRoot(start string) (string, error) {
	dir := start
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .git directory found ascending from %s", start)
		}
		dir = parent
	}
}

// expandTilde expands a leading `~/` or `~` to the user's home directory.
func expandTilde(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

// EqualSourceRepos reports whether two source-repo paths refer to the same
// physical location after canonicalization. Both arguments may be in any
// pre-canonical form (relative, symlinked, with `~`).
func EqualSourceRepos(a, b string) bool {
	if a == b {
		return true
	}
	return canonicalize(a) == canonicalize(b)
}
