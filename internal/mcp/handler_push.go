package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type pushInput struct {
	TaskID         string `json:"task_id"`
	ForceWithLease bool   `json:"force_with_lease,omitempty"`
	Branch         string `json:"branch,omitempty"`
	Remote         string `json:"remote,omitempty"`
}

// NewPushHandler returns a Handler that decodes the envelope input and
// calls verbs.Push.
func NewPushHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in pushInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:push: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:push: task_id is required")
		}

		result, err := verbs.Push(ctx, client, in.TaskID, verbs.PushOptions{ForceWithLease: in.ForceWithLease, Branch: in.Branch, Remote: in.Remote})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:push: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:push: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
