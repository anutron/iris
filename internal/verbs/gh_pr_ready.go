package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// GHPRReadyOptions captures the per-call knobs for GHPRReady.
type GHPRReadyOptions struct {
	PRNumber int
}

// GHPRReadyResult is the structured success payload.
type GHPRReadyResult struct {
	Ready    bool `json:"ready"`
	WasDraft bool `json:"was_draft"`
}

// GHPRReady takes a draft GitHub PR out of draft via the host gh CLI.
// Pre-fetches the PR's isDraft so the result can report whether the call
// actually moved state. Holds the per-source-repo lock for the duration
// of both gh shellouts.
func GHPRReady(ctx context.Context, client *argus.Client, taskID string, opts GHPRReadyOptions) (*GHPRReadyResult, error) {
	if opts.PRNumber <= 0 {
		return nil, fmt.Errorf("pr_number must be a positive integer")
	}

	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	// Pre-fetch isDraft. A failure here surfaces with full gh output —
	// the agent likely can't proceed without knowing PR state anyway.
	viewArgs := []string{"pr", "view", fmt.Sprintf("%d", opts.PRNumber), "--json", "isDraft"}
	viewCmd := exec.CommandContext(ctx, "gh", viewArgs...)
	viewCmd.Dir = resolved.SourceRepo
	viewOut, err := viewCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr view (pre-fetch isDraft): %w: %s", err, strings.TrimSpace(string(viewOut)))
	}
	var pre struct {
		IsDraft bool `json:"isDraft"`
	}
	if err := json.Unmarshal(viewOut, &pre); err != nil {
		return nil, fmt.Errorf("parse pre-fetch isDraft JSON: %w; output:\n%s", err, string(viewOut))
	}

	readyArgs := []string{"pr", "ready", fmt.Sprintf("%d", opts.PRNumber)}
	readyCmd := exec.CommandContext(ctx, "gh", readyArgs...)
	readyCmd.Dir = resolved.SourceRepo
	readyOut, err := readyCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr ready: %w: %s", err, strings.TrimSpace(string(readyOut)))
	}

	return &GHPRReadyResult{Ready: true, WasDraft: pre.IsDraft}, nil
}
