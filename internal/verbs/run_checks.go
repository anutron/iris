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

// RunChecksOptions captures the per-call knobs for RunChecks.
type RunChecksOptions struct {
	// Check is the check to run (e.g. "lint", "test", "security"). It is
	// passed as the first positional argument to `script/iris-check`.
	// Required and non-empty — it is a single token handed to a
	// repo-controlled script, NOT a shell string.
	Check string
}

// RunChecksResult is the structured success payload. Populated even when
// the check exits non-zero — callers can read Output and ExitCode via
// errors.As(*CheckExitError).
type RunChecksResult struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

// CheckExitError wraps a populated *RunChecksResult when the check command
// ran but exited non-zero. Callers use `errors.As(err, &checkErr)` to
// inspect ExitCode and Output without a second lookup.
type CheckExitError struct {
	Result *RunChecksResult
	Err    error // underlying *exec.ExitError or wrap of it
}

// Error implements the error interface.
func (e *CheckExitError) Error() string {
	if e.Result == nil {
		return fmt.Sprintf("check failed: %v", e.Err)
	}
	return fmt.Sprintf("check failed: %s exited %d", e.Result.Command, e.Result.ExitCode)
}

// Unwrap supports errors.Is/As against the underlying exit error.
func (e *CheckExitError) Unwrap() error { return e.Err }

// RunChecks resolves the task's worktree, runs the repo-defined check
// script (`script/iris-check <check>`) in the worktree under a
// per-worktree mutex, and returns its captured output.
//
// Unlike RunBuild there is NO Makefile fallback: checks are script-only.
// If `script/iris-check` is absent or non-executable, RunChecks returns an
// actionable error naming the expected path.
//
// On successful exit (code 0): returns (*RunChecksResult, nil).
// On non-zero exit: returns (*RunChecksResult, *CheckExitError) — the
// result is populated so callers see the captured output and exit code.
// On any other failure (empty check, resolve, missing/non-executable
// script, process couldn't start): returns (nil, error).
func RunChecks(ctx context.Context, client *argus.Client, taskID string, opts RunChecksOptions) (*RunChecksResult, error) {
	if opts.Check == "" {
		return nil, fmt.Errorf("check is required and must be non-empty")
	}

	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	scriptPath := filepath.Join(resolved.WorktreePath, "script", "iris-check")
	if !isExecutable(scriptPath) {
		return nil, fmt.Errorf(
			"no check mechanism: expected executable %s in worktree %s",
			scriptPath, resolved.WorktreePath,
		)
	}

	display := "script/iris-check " + opts.Check

	mu := lockWorktree(resolved.WorktreePath)
	defer mu.Unlock()

	// Load .iris.local.toml's [secrets] block (merged over .iris.toml) for
	// the canonical source repo — never the worktree, since a secret source
	// descriptor is per-developer, not per-checkout. This is fail-open by
	// design (see design.md's "Fail-open" goal and the merge_to_branch.go
	// "skip hook, warn, continue" precedent): an I/O error loading config,
	// or no .iris.toml at all, must never block the check itself. Only the
	// reason is ever logged — never a config value, and never a resolved
	// secret.
	var secretsBlock config.SecretsBlock
	overlay, cfgErr := config.LoadOverlay(resolved.SourceRepo, false)
	switch {
	case cfgErr != nil:
		slog.Warn("secrets injection skipped: could not load .iris.toml",
			"source_repo", resolved.SourceRepo,
			"err", cfgErr,
		)
	case overlay.Doc == nil:
		slog.Warn("secrets injection skipped: no .iris.toml found",
			"source_repo", resolved.SourceRepo,
		)
	default:
		secretsBlock = overlay.Doc.Secrets
	}

	cmd := exec.CommandContext(ctx, scriptPath, opts.Check)
	cmd.Dir = resolved.WorktreePath
	cmd.Env = append(os.Environ(), secrets.ResolveEnv(ctx, secretsBlock)...)
	out, runErr := cmd.CombinedOutput()

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// The process failed to start (binary missing, fork error,
			// context cancelled before exec). No structured result.
			return nil, fmt.Errorf("run check %s in %s: %w; output:\n%s",
				display, resolved.WorktreePath, runErr, strings.TrimSpace(string(out)))
		}
	}

	result := &RunChecksResult{
		Command:  display,
		ExitCode: exitCode,
		Output:   string(out),
	}

	if exitCode != 0 {
		return result, &CheckExitError{Result: result, Err: runErr}
	}
	return result, nil
}
