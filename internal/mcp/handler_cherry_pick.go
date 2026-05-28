package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type cherryPickInput struct {
	TaskID       string `json:"task_id"`
	Commit       string `json:"commit"`
	TargetBranch string `json:"target_branch"`
}

// NewCherryPickHandler returns a Handler that decodes the envelope input
// and calls verbs.CherryPick.
func NewCherryPickHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in cherryPickInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:cherry_pick: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:cherry_pick: task_id is required")
		}
		if in.Commit == "" {
			return ErrorResponse("iris:cherry_pick: commit is required")
		}
		if in.TargetBranch == "" {
			return ErrorResponse("iris:cherry_pick: target_branch is required")
		}

		result, err := verbs.CherryPick(ctx, verbs.CherryPickInput{
			Client: client, TaskID: in.TaskID, Commit: in.Commit, TargetBranch: in.TargetBranch,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:cherry_pick: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:cherry_pick: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
