package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type ghPRCommentInput struct {
	TaskID   string `json:"task_id"`
	PRNumber int    `json:"pr_number"`
	Body     string `json:"body"`
}

// NewGHPRCommentHandler returns a Handler that decodes the envelope input
// and calls verbs.GHPRComment.
func NewGHPRCommentHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in ghPRCommentInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:gh_pr_comment: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:gh_pr_comment: task_id is required")
		}
		if in.PRNumber <= 0 {
			return ErrorResponse("iris:gh_pr_comment: pr_number must be a positive integer")
		}
		if strings.TrimSpace(in.Body) == "" {
			return ErrorResponse("iris:gh_pr_comment: body is required")
		}

		result, err := verbs.GHPRComment(ctx, client, in.TaskID, verbs.GHPRCommentOptions{
			PRNumber: in.PRNumber,
			Body:     in.Body,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_comment: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_comment: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
