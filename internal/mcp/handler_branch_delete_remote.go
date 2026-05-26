package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type branchDeleteRemoteInput struct {
	TaskID string `json:"task_id"`
	Branch string `json:"branch"`
}

// NewBranchDeleteRemoteHandler returns a Handler that decodes the
// envelope input and calls verbs.BranchDeleteRemote.
func NewBranchDeleteRemoteHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in branchDeleteRemoteInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:branch_delete_remote: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:branch_delete_remote: task_id is required")
		}
		if in.Branch == "" {
			return ErrorResponse("iris:branch_delete_remote: branch is required")
		}

		result, err := verbs.BranchDeleteRemote(ctx, verbs.BranchDeleteRemoteInput{
			Client: client, TaskID: in.TaskID, Branch: in.Branch,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:branch_delete_remote: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:branch_delete_remote: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
