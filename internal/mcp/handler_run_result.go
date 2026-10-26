package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

type runResultInput struct {
	RunID       string `json:"run_id"`
	WaitSeconds *int   `json:"wait_seconds,omitempty"`
}

// NewRunResultHandler returns a Handler that long-polls a transfer or worktree
// run. A finished run returns the
// response the original tool would have returned synchronously.
func NewRunResultHandler(runs *RunRegistry) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in runResultInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:run_result: invalid input: %v", err))
			}
		}
		if in.RunID == "" {
			return ErrorResponse("iris:run_result: run_id is required")
		}
		run := runs.Get(in.RunID)
		if run == nil {
			return ErrorResponse(fmt.Sprintf(
				"iris:run_result: unknown run_id %q. Iris keeps runs in memory for an hour after they finish, so it may have restarted or the run expired. The outcome is unknown: verify remote state before retrying a push or fetch; re-run a check or build as needed.",
				in.RunID,
			))
		}
		return runs.Respond(ctx, run, ClampRunWait(in.WaitSeconds))
	})
}
