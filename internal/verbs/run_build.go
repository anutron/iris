package verbs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
	"github.com/anutron/iris/internal/secrets"
)

// RunBuildOptions captures the per-call knobs for RunBuild.
type RunBuildOptions struct {
	// Target, if non-empty, is passed as the first positional argument
	// to the build command (e.g., `script/iris-build release` or
	// `make build release`).
	Target string
}

// RunBuildResult is the structured success payload. Populated even when
// the build exits non-zero — callers can read Output and ExitCode via
// errors.As(*BuildExitError).
type RunBuildResult struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

// BuildExitError wraps a populated *RunBuildResult when the build command
// ran but exited non-zero. Callers use `errors.As(err, &buildErr)` to
// inspect ExitCode and Output without a second lookup.
type BuildExitError struct {
	Result *RunBuildResult
	Err    error // underlying *exec.ExitError or wrap of it
}

// Error implements the error interface.
func (e *BuildExitError) Error() string {
	if e.Result == nil {
		return fmt.Sprintf("build failed: %v", e.Err)
	}
	return fmt.Sprintf("build failed: %s exited %d", e.Result.Command, e.Result.ExitCode)
}

// Unwrap supports errors.Is/As against the underlying exit error.
func (e *BuildExitError) Unwrap() error { return e.Err }

// RunBuild resolves the task's worktree, discovers the build command by
// convention (`script/iris-build` then `Makefile`), and runs it in the
// worktree under a per-worktree mutex.
//
// On successful exit (code 0): returns (*RunBuildResult, nil).
// On non-zero exit: returns (*RunBuildResult, *BuildExitError) — the
// result is populated so callers see the captured output and exit code.
// On any other failure (resolve, allowlist, missing build mechanism,
// process couldn't start): returns (nil, error).
func RunBuild(ctx context.Context, client *argus.Client, taskID string, opts RunBuildOptions) (*RunBuildResult, error) {
	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	scriptPath := filepath.Join(resolved.WorktreePath, "script", "iris-build")
	makefilePath := filepath.Join(resolved.WorktreePath, "Makefile")

	var (
		cmdName string
		cmdArgs []string
		display string
	)

	switch {
	case isExecutable(scriptPath):
		cmdName = scriptPath
		if opts.Target != "" {
			cmdArgs = []string{opts.Target}
			display = "script/iris-build " + opts.Target
		} else {
			display = "script/iris-build"
		}
	case fileExists(makefilePath):
		cmdName = "make"
		cmdArgs = []string{"build"}
		if opts.Target != "" {
			cmdArgs = append(cmdArgs, opts.Target)
			display = "make build " + opts.Target
		} else {
			display = "make build"
		}
	default:
		return nil, fmt.Errorf(
			"no build mechanism: expected executable %s or %s in worktree %s",
			scriptPath, makefilePath, resolved.WorktreePath,
		)
	}

	// Resolve [secrets.env] for the resolved source repo, fail-open: a
	// config-load problem (I/O error or nil Doc — the latter covering both
	// "no .iris.toml at all" and "shared file failed to parse") never
	// blocks the build, it just means no secrets get injected. Mirrors
	// merge_to_branch.go's "post_merge hook skipped: could not load
	// .iris.toml" fail-open precedent. Only the reason is ever logged —
	// never a resolved secret value.
	var secretEnv []string
	overlay, loadErr := config.LoadOverlay(resolved.SourceRepo, false)
	switch {
	case loadErr != nil:
		slog.Warn("secrets resolution skipped: could not load .iris.toml",
			"source_repo", resolved.SourceRepo,
			"err", loadErr,
		)
	case overlay.Doc == nil:
		slog.Warn("secrets resolution skipped: no .iris.toml found",
			"source_repo", resolved.SourceRepo,
		)
	default:
		secretEnv = secrets.ResolveEnv(ctx, overlay.Doc.Secrets)
	}

	mu := lockWorktree(resolved.WorktreePath)
	defer mu.Unlock()

	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	cmd.Dir = resolved.WorktreePath
	// cmd.Env is nil by default (Go inherits the whole process
	// environment); seed it explicitly from os.Environ() so appending the
	// resolved secrets doesn't silently drop that inheritance.
	cmd.Env = append(os.Environ(), secretEnv...)
	out, runErr := cmd.CombinedOutput()

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// The process failed to start (binary missing, fork error,
			// context cancelled before exec). No structured result.
			return nil, fmt.Errorf("run build %s in %s: %w; output:\n%s",
				display, resolved.WorktreePath, runErr, strings.TrimSpace(string(out)))
		}
	}

	result := &RunBuildResult{
		Command:  display,
		ExitCode: exitCode,
		Output:   string(out),
	}

	if exitCode != 0 {
		return result, &BuildExitError{Result: result, Err: runErr}
	}
	return result, nil
}

// isExecutable reports whether the file exists and has any of the
// owner/group/world execute bits set.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

// fileExists reports whether the path exists as a regular file (not a directory).
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
