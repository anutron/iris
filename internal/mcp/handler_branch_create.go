package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type branchCreateInput struct {
	TaskID  string `json:"task_id"`
	Name    string `json:"name"`
	BaseRef string `json:"base_ref"`
}

// NewBranchCreateHandler returns a Handler that decodes the envelope input
// and calls verbs.BranchCreate.
func NewBranchCreateHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in branchCreateInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:branch_create: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:branch_create: task_id is required")
		}
		if in.Name == "" {
			return ErrorResponse("iris:branch_create: name is required")
		}
		if in.BaseRef == "" {
			return ErrorResponse("iris:branch_create: base_ref is required")
		}

		result, err := verbs.BranchCreate(ctx, verbs.BranchCreateInput{
			Client: client, TaskID: in.TaskID, Name: in.Name, BaseRef: in.BaseRef,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:branch_create: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:branch_create: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
