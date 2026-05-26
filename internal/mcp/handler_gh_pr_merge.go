package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type ghPRMergeInput struct {
	TaskID   string `json:"task_id"`
	PRNumber int    `json:"pr_number"`
	Strategy string `json:"strategy,omitempty"`
}

// NewGHPRMergeHandler returns a Handler that decodes the envelope input
// and calls verbs.GHPRMerge.
func NewGHPRMergeHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in ghPRMergeInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:gh_pr_merge: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:gh_pr_merge: task_id is required")
		}
		if in.PRNumber <= 0 {
			return ErrorResponse("iris:gh_pr_merge: pr_number must be a positive integer")
		}
		if in.Strategy == "" {
			in.Strategy = "squash"
		}
		if !verbs.IsValidGHPRMergeStrategy(in.Strategy) {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_merge: invalid strategy %q (must be one of squash|merge|rebase)", in.Strategy))
		}

		result, err := verbs.GHPRMerge(ctx, client, in.TaskID, verbs.GHPRMergeOptions{
			PRNumber: in.PRNumber,
			Strategy: in.Strategy,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_merge: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_merge: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
