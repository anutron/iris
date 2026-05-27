package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type ghPRReadyInput struct {
	TaskID   string `json:"task_id"`
	PRNumber int    `json:"pr_number"`
}

// NewGHPRReadyHandler returns a Handler that decodes the envelope input
// and calls verbs.GHPRReady.
func NewGHPRReadyHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in ghPRReadyInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:gh_pr_ready: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:gh_pr_ready: task_id is required")
		}
		if in.PRNumber <= 0 {
			return ErrorResponse("iris:gh_pr_ready: pr_number must be a positive integer")
		}

		result, err := verbs.GHPRReady(ctx, client, in.TaskID, verbs.GHPRReadyOptions{PRNumber: in.PRNumber})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_ready: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_ready: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
