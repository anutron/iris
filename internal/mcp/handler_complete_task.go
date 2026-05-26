package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type completeTaskInput struct {
	TaskID        string `json:"task_id"`
	MergeStrategy string `json:"merge_strategy,omitempty"`
}

// NewCompleteTaskHandler returns a Handler that decodes the envelope input
// and calls verbs.CompleteTask. On partial failure the response is an
// ErrorResponse whose text body is the marshaled result (checkpoints +
// error) so the caller can see how far the flow got.
func NewCompleteTaskHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in completeTaskInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:complete_task: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:complete_task: task_id is required")
		}

		result, err := verbs.CompleteTask(ctx, client, in.TaskID, verbs.CompleteTaskOptions{MergeStrategy: in.MergeStrategy})
		if err != nil {
			// Leading line is the human-readable summary; JSON payload
			// gives the agent structured checkpoint data without
			// duplicating the error text.
			var checkpoints []string
			if result != nil {
				checkpoints = result.Checkpoints
			}
			body, _ := json.MarshalIndent(map[string]any{"checkpoints": checkpoints}, "", "  ")
			return ErrorResponse(fmt.Sprintf("iris:complete_task: %v\n%s", err, body))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:complete_task: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
