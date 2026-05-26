package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// GHPRViewOptions captures the per-call knobs for GHPRView.
type GHPRViewOptions struct {
	PRNumber int
}

// GHPRViewResult is the structured success payload. Data is the parsed
// JSON object returned by `gh pr view --json …`. Iris does not interpret
// the fields; callers decode the shape they need.
type GHPRViewResult struct {
	Data map[string]any `json:"data"`
}

// ghPRViewJSONFields is the fixed --json field list iris requests. The
// design pins this so callers see a stable shape regardless of which
// agent invoked the verb.
const ghPRViewJSONFields = "state,checks,reviews,mergeable,headRefName,baseRefName,isDraft,statusCheckRollup"

// GHPRView reads a GitHub PR's state via the host gh CLI in the resolved
// source repo and returns the parsed JSON. Holds the per-source-repo lock
// for the duration of the gh shellout.
func GHPRView(ctx context.Context, client *argus.Client, taskID string, opts GHPRViewOptions) (*GHPRViewResult, error) {
	if opts.PRNumber <= 0 {
		return nil, fmt.Errorf("pr_number must be a positive integer")
	}

	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	args := []string{"pr", "view", fmt.Sprintf("%d", opts.PRNumber), "--json", ghPRViewJSONFields}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = resolved.SourceRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w: %s", err, strings.TrimSpace(string(out)))
	}

	var data map[string]any
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, fmt.Errorf("parse gh pr view JSON: %w; output:\n%s", err, string(out))
	}
	return &GHPRViewResult{Data: data}, nil
}
