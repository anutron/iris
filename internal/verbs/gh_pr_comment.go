package verbs

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// GHPRCommentOptions captures the per-call knobs for GHPRComment.
type GHPRCommentOptions struct {
	PRNumber int
	Body     string
}

// GHPRCommentResult is the structured success payload. ParseWarning is
// populated when gh exited 0 but its stdout did not contain a parseable
// comment URL; URL is empty in that case and the raw stdout is in
// ParseWarning so callers can recover.
type GHPRCommentResult struct {
	URL          string `json:"url"`
	ParseWarning string `json:"parse_warning,omitempty"`
}

// ghPRCommentURLRegexp matches the URL gh emits after posting a comment.
// Format: https://github.com/<owner>/<repo>/pull/<n>#issuecomment-<id>
var ghPRCommentURLRegexp = regexp.MustCompile(`https?://[^\s]+#issuecomment-\d+`)

// GHPRComment posts a comment to a GitHub PR via the host gh CLI. Refuses
// empty/whitespace bodies before shelling out. Holds the per-source-repo
// lock for the duration of the gh shellout.
func GHPRComment(ctx context.Context, client *argus.Client, taskID string, opts GHPRCommentOptions) (*GHPRCommentResult, error) {
	if opts.PRNumber <= 0 {
		return nil, fmt.Errorf("pr_number must be a positive integer")
	}
	if strings.TrimSpace(opts.Body) == "" {
		return nil, fmt.Errorf("body is required (no zero-length comments)")
	}

	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	args := []string{"pr", "comment", fmt.Sprintf("%d", opts.PRNumber), "--body", opts.Body}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = resolved.SourceRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr comment: %w: %s", err, strings.TrimSpace(string(out)))
	}

	if url := ghPRCommentURLRegexp.FindString(string(out)); url != "" {
		return &GHPRCommentResult{URL: url}, nil
	}
	return &GHPRCommentResult{ParseWarning: strings.TrimSpace(string(out))}, nil
}
