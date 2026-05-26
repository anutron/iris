package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type tagInput struct {
	TaskID  string `json:"task_id"`
	Tag     string `json:"tag"`
	Message string `json:"message,omitempty"`
}

// NewTagHandler returns a Handler that decodes the envelope input and
// calls verbs.Tag.
func NewTagHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in tagInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:tag: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:tag: task_id is required")
		}
		if in.Tag == "" {
			return ErrorResponse("iris:tag: tag is required")
		}

		result, err := verbs.Tag(ctx, verbs.TagInput{
			Client: client, TaskID: in.TaskID, Tag: in.Tag, Message: in.Message,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:tag: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:tag: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
