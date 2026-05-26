package verbs

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// GHPRMergeOptions captures the per-call knobs for GHPRMerge.
type GHPRMergeOptions struct {
	PRNumber int
	Strategy string // "squash" | "merge" | "rebase"
}

// GHPRMergeResult is the structured success payload.
type GHPRMergeResult struct {
	Merged   bool   `json:"merged"`
	Strategy string `json:"strategy"`
}

// validGHPRMergeStrategies bounds the strategy enum at the verb boundary.
// Re-checked in the MCP handler so an invalid value is rejected before
// any subprocess is touched.
var validGHPRMergeStrategies = map[string]struct{}{
	"squash": {},
	"merge":  {},
	"rebase": {},
}

// IsValidGHPRMergeStrategy reports whether s is an accepted strategy.
// Exported so the MCP handler can validate without duplicating the set.
func IsValidGHPRMergeStrategy(s string) bool {
	_, ok := validGHPRMergeStrategies[s]
	return ok
}

// GHPRMerge merges a GitHub pull request via the host's gh CLI. v1 does
// NOT gate on CI status: gh's own merge command returns non-zero when CI
// is required and red, so the caller sees the failure that way. Adding
// an explicit pre-check is non-breaking and tracked in design.md.
func GHPRMerge(ctx context.Context, client *argus.Client, taskID string, opts GHPRMergeOptions) (*GHPRMergeResult, error) {
	if opts.PRNumber <= 0 {
		return nil, fmt.Errorf("pr_number must be a positive integer")
	}
	if !IsValidGHPRMergeStrategy(opts.Strategy) {
		return nil, fmt.Errorf("invalid strategy %q (must be one of squash|merge|rebase)", opts.Strategy)
	}

	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	args := []string{"pr", "merge", fmt.Sprintf("%d", opts.PRNumber), "--" + opts.Strategy}

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = resolved.SourceRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr merge: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return &GHPRMergeResult{Merged: true, Strategy: opts.Strategy}, nil
}
