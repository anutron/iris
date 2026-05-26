package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type ghPRCreateInput struct {
	TaskID string `json:"task_id"`
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	Draft  bool   `json:"draft,omitempty"`
}

// NewGHPRCreateHandler returns a Handler that decodes the envelope input
// and calls verbs.GHPRCreate.
func NewGHPRCreateHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in ghPRCreateInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:gh_pr_create: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:gh_pr_create: task_id is required")
		}
		if strings.TrimSpace(in.Title) == "" {
			return ErrorResponse("iris:gh_pr_create: title is required")
		}

		result, err := verbs.GHPRCreate(ctx, client, in.TaskID, verbs.GHPRCreateOptions{
			Title: in.Title,
			Body:  in.Body,
			Draft: in.Draft,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_create: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:gh_pr_create: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
