package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type fetchInput struct {
	TaskID      string `json:"task_id"`
	WaitSeconds *int   `json:"wait_seconds,omitempty"`
}

// NewFetchHandler returns a Handler that decodes the envelope input and
// calls verbs.Fetch as a detached, pollable run.
func NewFetchHandler(client *argus.Client, runs *RunRegistry) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		// Count preflight resolution against the callback budget too.
		ctx, cancel := context.WithTimeout(ctx, MaxRunWaitSeconds*time.Second)
		defer cancel()
		var in fetchInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:fetch: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:fetch: task_id is required")
		}

		if _, err := verbs.Resolve(ctx, client, in.TaskID); err != nil {
			return ErrorResponse(fmt.Sprintf("iris:fetch: %v", err))
		}
		run := runs.Start(ctx, "iris_fetch", in.TaskID, "", func(ctx context.Context) Response {
			return fetch(ctx, client, in.TaskID)
		})
		return runs.Respond(ctx, run, ClampRunWait(in.WaitSeconds))
	})
}

func fetch(ctx context.Context, client *argus.Client, taskID string) Response {
	result, err := verbs.Fetch(ctx, verbs.FetchInput{Client: client, TaskID: taskID})
	if err != nil {
		return ErrorResponse(fmt.Sprintf("iris:fetch: %v", err))
	}

	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return ErrorResponse(fmt.Sprintf("iris:fetch: encode result: %v", err))
	}
	return TextResponse(string(body))
}
