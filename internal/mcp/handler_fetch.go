package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type fetchInput struct {
	TaskID string `json:"task_id"`
}

// NewFetchHandler returns a Handler that decodes the envelope input and
// calls verbs.Fetch.
func NewFetchHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in fetchInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:fetch: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:fetch: task_id is required")
		}

		result, err := verbs.Fetch(ctx, verbs.FetchInput{Client: client, TaskID: in.TaskID})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:fetch: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:fetch: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
