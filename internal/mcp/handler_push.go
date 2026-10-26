package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type pushInput struct {
	TaskID         string `json:"task_id"`
	ForceWithLease bool   `json:"force_with_lease,omitempty"`
	Branch         string `json:"branch,omitempty"`
	Remote         string `json:"remote,omitempty"`
	WaitSeconds    *int   `json:"wait_seconds,omitempty"`
}

// NewPushHandler returns a Handler that decodes the envelope input and
// calls verbs.Push as a detached, pollable run.
func NewPushHandler(client *argus.Client, runs *RunRegistry) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		// Count preflight resolution against the callback budget too.
		ctx, cancel := context.WithTimeout(ctx, MaxRunWaitSeconds*time.Second)
		defer cancel()
		var in pushInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:push: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:push: task_id is required")
		}

		if _, err := verbs.Resolve(ctx, client, in.TaskID); err != nil {
			return ErrorResponse(fmt.Sprintf("iris:push: %v", err))
		}
		// Only transfer options affect identity; changing the wait joins
		// the existing transfer rather than enqueueing another push.
		opts := verbs.PushOptions{ForceWithLease: in.ForceWithLease, Branch: in.Branch, Remote: in.Remote}
		key, _ := json.Marshal(opts)
		run := runs.Start(ctx, "iris_push", in.TaskID, string(key), func(ctx context.Context) Response {
			return push(ctx, client, in.TaskID, opts)
		})
		return runs.Respond(ctx, run, ClampRunWait(in.WaitSeconds))
	})
}

func push(ctx context.Context, client *argus.Client, taskID string, opts verbs.PushOptions) Response {
	result, err := verbs.Push(ctx, client, taskID, opts)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("iris:push: %v", err))
	}

	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return ErrorResponse(fmt.Sprintf("iris:push: encode result: %v", err))
	}
	return TextResponse(string(body))
}
