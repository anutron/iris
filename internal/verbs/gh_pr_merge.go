package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/hostexec"
)

// GHPRMergeOptions captures the per-call knobs for GHPRMerge.
type GHPRMergeOptions struct {
	PRNumber int
	Strategy string // "squash" | "merge" | "rebase"
	// ExpectedRepo, when set, is the "owner/name" the PR must belong to;
	// the merge is refused on a mismatch.
	ExpectedRepo string
}

// GHPRMergeResult is the structured success payload.
type GHPRMergeResult struct {
	Merged        bool     `json:"merged"`
	Strategy      string   `json:"strategy"`
	PRURL         string   `json:"pr_url,omitempty"`
	PRTitle       string   `json:"pr_title,omitempty"`
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
	if err := validateExpectedRepo(opts.ExpectedRepo); err != nil {
		return nil, err
	}

	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	id, idWarning, err := identifyPR(ctx, resolved.SourceRepo, opts.PRNumber, opts.ExpectedRepo)
	if err != nil {
		return nil, err
	}

	args := []string{"pr", "merge", fmt.Sprintf("%d", opts.PRNumber), "--" + opts.Strategy}
	args = append(args, repoArgs(id)...)

	cmd := hostexec.CommandContext(ctx, nil, "gh", args...)
	cmd.Dir = resolved.SourceRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr merge: %w: %s", err, strings.TrimSpace(string(out)))
	}

	result := &GHPRMergeResult{Merged: true, Strategy: opts.Strategy}
	if id != nil {
		result.PRURL, result.PRTitle = id.URL, id.Title
	}
	if idWarning != "" {
		result.Warnings = append(result.Warnings, idWarning)
	}

	// After merging on GitHub, bring the local default branch up to date.
	defaultBranch, dbErr := DefaultBranch(ctx, resolved.SourceRepo)
	if dbErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("post-merge: could not determine default branch: %v", dbErr))
		return result, nil
	}
	result.DefaultBranch = defaultBranch

	// Never move the host checkout: it may be serving a dogfood branch. Sync
	// the local default branch without a checkout, under the source-repo lock
	// and the git transfer timeout.
	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()
	result.Warnings = append(result.Warnings, syncDefaultBranchNoCheckout(ctx, resolved.SourceRepo, defaultBranch)...)
	return result, nil
}
