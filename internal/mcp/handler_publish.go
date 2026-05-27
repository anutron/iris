package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type publishInput struct {
	TaskID string `json:"task_id"`
	Branch string `json:"branch,omitempty"`
	Push   bool   `json:"push,omitempty"`
	Reset  bool   `json:"reset,omitempty"`
}

// NewPublishHandler returns a Handler for iris_publish.
func NewPublishHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in publishInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:publish: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:publish: task_id is required")
		}
		caller := callerFromContext(ctx)
		if caller == "" {
			caller = in.TaskID
		}
		result, err := verbs.Publish(ctx, client, verbs.PublishInput{
			TaskID: in.TaskID,
			Branch: in.Branch,
			Push:   in.Push,
			Reset:  in.Reset,
			Caller: caller,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:publish: %v", err))
		}
		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:publish: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
