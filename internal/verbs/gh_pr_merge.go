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
	Merged        bool     `json:"merged"`
	Strategy      string   `json:"strategy"`
	DefaultBranch string   `json:"default_branch,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
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

	result := &GHPRMergeResult{Merged: true, Strategy: opts.Strategy}

	// After merging on GitHub, restore the canonical source repo to the default
	// branch and pull so a subsequent iris_reload (which requires default-branch
	// checkout) works without manual intervention.
	defaultBranch, dbErr := DefaultBranch(ctx, resolved.SourceRepo)
	if dbErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("post-merge: could not determine default branch: %v", dbErr))
		return result, nil
	}
	result.DefaultBranch = defaultBranch

	current, cbErr := currentBranch(ctx, resolved.SourceRepo)
	if cbErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("post-merge: could not read current branch: %v", cbErr))
		return result, nil
	}

	if current != defaultBranch {
		if _, coErr := runGit(ctx, resolved.SourceRepo, "checkout", defaultBranch); coErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("post-merge: checkout %q failed: %v", defaultBranch, coErr))
			return result, nil
		}
	}

	if _, pullErr := runGit(ctx, resolved.SourceRepo, "pull", "--ff-only"); pullErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("post-merge: pull --ff-only failed: %v", pullErr))
	}

	return result, nil
}
