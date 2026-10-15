package verbs

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runDeploymentGit bounds each git step in an already-detached deployment.
// WaitDelay also bounds inherited output pipes after timeout or process exit.
// A separate budget per command leaves the build/restart budgets independent.
func runDeploymentGit(ctx context.Context, dir string, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, gitTransferTimeout(dir))
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "git", append([]string{"-C", dir}, args...)...)
	cmd.WaitDelay = gitTransferWaitDelay
	out, err := cmd.CombinedOutput()
	if commandCtx.Err() != nil {
		err = commandCtx.Err()
	}
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), err
}
