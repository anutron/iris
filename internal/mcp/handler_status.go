package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type statusInput struct {
	TaskID string `json:"task_id,omitempty"`
	Path   string `json:"path,omitempty"`
}

// NewStatusHandler returns a Handler for iris_status.
func NewStatusHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in statusInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:status: invalid input: %v", err))
			}
		}
		result, err := verbs.Status(ctx, client, verbs.StatusInput{
			TaskID: in.TaskID, Path: in.Path,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:status: %v", err))
		}
		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:status: encode: %v", err))
		}
		return TextResponse(string(body))
	})
}
