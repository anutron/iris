package verbs

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// GHPRCloseOptions captures the per-call knobs for GHPRClose.
type GHPRCloseOptions struct {
	PRNumber     int
	DeleteBranch bool
}

// GHPRCloseResult is the structured success payload. BranchDeleted echoes
// the input flag — gh deletes the branch when --delete-branch is passed
// and reports failure non-zero on its own, so this verb does not parse
// gh's stdout for confirmation.
type GHPRCloseResult struct {
	Closed        bool `json:"closed"`
	BranchDeleted bool `json:"branch_deleted"`
}

// GHPRClose closes a GitHub PR without merging via the host gh CLI.
// Optionally deletes the source branch (via gh's --delete-branch). Holds
// the per-source-repo lock for the duration of the gh shellout.
func GHPRClose(ctx context.Context, client *argus.Client, taskID string, opts GHPRCloseOptions) (*GHPRCloseResult, error) {
	if opts.PRNumber <= 0 {
		return nil, fmt.Errorf("pr_number must be a positive integer")
	}

	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	args := []string{"pr", "close", fmt.Sprintf("%d", opts.PRNumber)}
	if opts.DeleteBranch {
		args = append(args, "--delete-branch")
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = resolved.SourceRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr close: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return &GHPRCloseResult{Closed: true, BranchDeleted: opts.DeleteBranch}, nil
}
