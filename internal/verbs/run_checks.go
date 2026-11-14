package verbs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
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

	// Args are appended after Check as separate argv entries (typically test
	// paths). They are validated by ValidateCheckArgs and never reach a
	// shell.
	Args []string
}

const (
	// MaxCheckArgs caps len(RunChecksOptions.Args). Real runs pass one to a
	// handful of paths; 64 leaves room for a CI-style shard.
	MaxCheckArgs = 64
	// MaxCheckArgBytes caps each arg's length, comfortably above any
	// repo-relative test path and below PATH_MAX.
	MaxCheckArgBytes = 512
)

// ValidateCheckArgs rejects args that are unsafe or malformed before
// anything runs: more than MaxCheckArgs entries; any entry that is empty,
// longer than MaxCheckArgBytes, contains a NUL byte, or starts with `-`
// (it would be parsed as an option by rspec/jest rather than a path).
func ValidateCheckArgs(args []string) error {
	if len(args) > MaxCheckArgs {
		return fmt.Errorf("args: at most %d args allowed, got %d", MaxCheckArgs, len(args))
	}
	for i, a := range args {
		switch {
		case a == "":
			return fmt.Errorf("args[%d]: an arg may not be empty", i)
		case len(a) > MaxCheckArgBytes:
			return fmt.Errorf("args[%d]: an arg may be at most %d bytes, got %d", i, MaxCheckArgBytes, len(a))
		case strings.ContainsRune(a, 0):
			return fmt.Errorf("args[%d]: an arg may not contain a NUL byte", i)
		case a[0] == '-':
			return fmt.Errorf("args[%d]: an arg may not start with `-` (got %q); pass a path like ./%s", i, a, a)
		}
	}
	return nil
}

// RunChecksResult is populated on command completion, including non-zero exit
// and timeout. Output is bounded; LogPath preserves an oversized combined log.
type RunChecksResult struct {
	Command                 string        `json:"command"`
	ExitCode                int           `json:"exit_code"`
	Output                  string        `json:"output"`
	OutputTruncated         bool          `json:"output_truncated,omitempty"`
	OutputBytes             int           `json:"output_bytes,omitempty"`
	LogPath                 string        `json:"log_path,omitempty"`
	Summary                 *CheckSummary `json:"summary,omitempty"`
	FailureSection          string        `json:"failure_section,omitempty"`
	FailureSectionTruncated bool          `json:"failure_section_truncated,omitempty"`
}

// CheckExitError wraps a populated *RunChecksResult when a command exits
// non-zero, times out, fails to start, or its full log cannot be saved.
// Callers use errors.As to inspect the bounded result without another lookup.
type CheckExitError struct {
	Result *RunChecksResult
	Err    error // execution or log persistence error
}

// Error implements the error interface.
func (e *CheckExitError) Error() string {
	if e.Result == nil {
		return fmt.Sprintf("check failed: %v", e.Err)
	}
	message := fmt.Sprintf("check failed: %s exited %d", e.Result.Command, e.Result.ExitCode)
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	if e.Result.ExitCode == -1 {
		message += "; output:\n" + e.Result.Output
	}
	return message
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
// On timeout, start failure, or log persistence failure: returns the bounded
// result with *CheckExitError. Pre-execution validation returns (nil, error).
func RunChecks(ctx context.Context, client *argus.Client, taskID string, opts RunChecksOptions) (*RunChecksResult, error) {
	if opts.Check == "" {
		return nil, fmt.Errorf("check is required and must be non-empty")
	}
	if err := ValidateCheckArgs(opts.Args); err != nil {
		return nil, err
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

	argv := append([]string{opts.Check}, opts.Args...)
	display := "script/iris-check " + strings.Join(argv, " ")

	// Load .iris.local.toml's [secrets] block (merged over .iris.toml) for
	// the canonical source repo — never the worktree, since a secret source
	// descriptor is per-developer, not per-checkout. This is fail-open by
	// design (see design.md's "Fail-open" goal and the merge_to_branch.go
	// "skip hook, warn, continue" precedent): an I/O error loading config
	// must never block the check itself. A simply-absent .iris.toml is the
	// normal, unconfigured state (matching LoadIrisToml's own "missing is
	// NOT an error" contract) and stays silent; only a config that exists
	// but failed to parse/validate, or a genuine I/O error, is worth a
	// warning. Only the reason is ever logged — never a config value, and
	// never a resolved secret.
	var secretsBlock config.SecretsBlock
	overlay, cfgErr := config.LoadOverlay(resolved.SourceRepo, false)
	switch {
	case cfgErr != nil:
		slog.Warn("secrets injection skipped: could not load .iris.toml",
			"source_repo", resolved.SourceRepo,
			"err", cfgErr,
		)
	case overlay.Doc == nil && len(overlay.ValidationErrors) > 0:
		slog.Warn("secrets injection skipped: .iris.toml failed to parse",
			"source_repo", resolved.SourceRepo,
			"errors", overlay.ValidationErrors,
		)
	case overlay.Doc == nil:
		// No .iris.toml at all — the normal, unconfigured case. Silent.
	default:
		secretsBlock = overlay.Doc.Secrets
	}

	env := append(os.Environ(), secrets.ResolveEnv(ctx, secretsBlock)...)
	out, exitCode, runErr := runWorktreeCommand(ctx, resolved.WorktreePath, env, scriptPath, argv...)
	result := &RunChecksResult{
		Command:  display,
		ExitCode: exitCode,
		Output:   string(out),
	}
	if err := boundCheckOutput(result); err != nil {
		return result, &CheckExitError{Result: result, Err: errors.Join(runErr, err)}
	}
	if runErr != nil {
		return result, &CheckExitError{Result: result, Err: fmt.Errorf("run check %s in %s: %w", display, resolved.WorktreePath, runErr)}
	}

	if exitCode != 0 {
		return result, &CheckExitError{Result: result, Err: fmt.Errorf("exit status %d", exitCode)}
	}
	return result, nil
}
