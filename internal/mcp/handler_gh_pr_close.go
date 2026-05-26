package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type ghPRCloseInput struct {
	TaskID       string `json:"task_id"`
	PRNumber     int    `json:"pr_number"`
	DeleteBranch bool   `json:"delete_branch,omitempty"`
}

// NewGHPRCloseHandler returns a Handler that decodes the envelope input
// and calls verbs.GHPRClose.
func NewGHPRCloseHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in ghPRCloseInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:gh_pr_close: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:gh_pr_close: task_id is required")
		}
		if in.PRNumber <= 0 {
			return ErrorResponse("iris:gh_pr_close: pr_number must be a positive integer")
		}

		result, err := verbs.GHPRClose(ctx, client, in.TaskID, verbs.GHPRCloseOptions{
			PRNumber:     in.PRNumber,
			DeleteBranch: in.DeleteBranch,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_close: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_close: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
