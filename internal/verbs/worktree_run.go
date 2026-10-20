package verbs

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// WorktreeRunTimeout is iris's ceiling on a run_checks or run_build
// subprocess. It is a hang guard, not a budget: the run is detached from
// the caller (whose MCP round trip argus caps at 30s), so without a ceiling
// a wedged script would hold the worktree lock forever.
const WorktreeRunTimeout = 30 * time.Minute

// worktreeRunTimeout is the effective ceiling; tests shrink it.
var worktreeRunTimeout = WorktreeRunTimeout

// worktreeRunWaitDelay bounds how long Wait drains output pipes after the
// script exits (or is killed). A grandchild that inherited the pipe (a
// backgrounded server, jest workers) would otherwise hold the run open.
const worktreeRunWaitDelay = time.Second

// runWorktreeCommand runs name+args in dir, detached from ctx's
// cancellation and bounded by worktreeRunTimeout. It returns the combined
// output and exit code. err is non-nil only when the command produced no
// exit status of its own: it failed to start, or it hit the ceiling (the
// error then starts with "[timeout]").
func runWorktreeCommand(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, int, error) {
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), worktreeRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.WaitDelay = worktreeRunWaitDelay
	out, err := cmd.CombinedOutput()

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return out, -1, fmt.Errorf("[timeout] exceeded iris's %s ceiling for worktree runs and was killed", worktreeRunTimeout)
	}
	if err == nil {
		return out, 0, nil
	}
	// The script exited 0 but a grandchild kept the output pipe open past
	// WaitDelay: the script's own outcome is success.
	if errors.Is(err, exec.ErrWaitDelay) {
		return out, cmd.ProcessState.ExitCode(), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out, exitErr.ExitCode(), nil
	}
	return out, -1, err
}
