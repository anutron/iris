package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type ghPRViewInput struct {
	TaskID   string `json:"task_id"`
	PRNumber int    `json:"pr_number"`
}

// NewGHPRViewHandler returns a Handler that decodes the envelope input
// and calls verbs.GHPRView. The success body is the raw JSON object gh
// returned (no envelope), so callers see exactly what `gh pr view --json`
// produced.
func NewGHPRViewHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in ghPRViewInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:gh_pr_view: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:gh_pr_view: task_id is required")
		}
		if in.PRNumber <= 0 {
			return ErrorResponse("iris:gh_pr_view: pr_number must be a positive integer")
		}

		result, err := verbs.GHPRView(ctx, client, in.TaskID, verbs.GHPRViewOptions{PRNumber: in.PRNumber})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_view: %v", err))
		}

		body, err := json.MarshalIndent(result.Data, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_view: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
