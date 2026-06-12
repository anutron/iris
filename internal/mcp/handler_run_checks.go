package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type runChecksInput struct {
	TaskID string `json:"task_id"`
	Check  string `json:"check"`
}

// NewRunChecksHandler returns a Handler that decodes the envelope input
// and calls verbs.RunChecks. On a non-zero check exit (the common
// lint/test-failure case), the handler surfaces an error response that
// includes the exit code and the captured output, so the agent sees the
// real rubocop/rspec/brakeman text without a second round-trip.
func NewRunChecksHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in runChecksInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:run_checks: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:run_checks: task_id is required")
		}
		if in.Check == "" {
			return ErrorResponse("iris:run_checks: check is required")
		}

		result, err := verbs.RunChecks(ctx, client, in.TaskID, verbs.RunChecksOptions{Check: in.Check})
		if err != nil {
			// If the check ran and exited non-zero, surface ExitCode and
			// Output in the error message so the agent sees the check
			// failures directly.
			var checkErr *verbs.CheckExitError
			if errors.As(err, &checkErr) && checkErr.Result != nil {
				return ErrorResponse(fmt.Sprintf(
					"iris:run_checks: %s exited %d\n%s",
					checkErr.Result.Command,
					checkErr.Result.ExitCode,
					checkErr.Result.Output,
				))
			}
			return ErrorResponse(fmt.Sprintf("iris:run_checks: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:run_checks: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
