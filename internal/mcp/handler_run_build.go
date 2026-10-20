package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type runBuildInput struct {
	TaskID      string `json:"task_id"`
	Target      string `json:"target,omitempty"`
	WaitSeconds *int   `json:"wait_seconds,omitempty"`
}

// NewRunBuildHandler returns a Handler that decodes the envelope input
// and calls verbs.RunBuild. On a non-zero build exit (the common compile-
// error case), the handler surfaces an error response that includes the
// exit code and the captured output, so the agent sees the build's
// stdout/stderr without a second round-trip.
//
// The build runs as a worktree run in runs: a build still going after
// wait_seconds returns a run_id handle for iris_run_result instead of
// outliving argus's 30s callback limit.
func NewRunBuildHandler(client *argus.Client, runs *RunRegistry) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in runBuildInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:run_build: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:run_build: task_id is required")
		}

		run := runs.Start(ctx, "iris_run_build", in.TaskID, in.Target, func(ctx context.Context) Response {
			return runBuild(ctx, client, in)
		})
		return runs.Respond(ctx, run, ClampRunWait(in.WaitSeconds))
	})
}

func runBuild(ctx context.Context, client *argus.Client, in runBuildInput) Response {
	result, err := verbs.RunBuild(ctx, client, in.TaskID, verbs.RunBuildOptions{Target: in.Target})
	if err != nil {
		// If the build ran and exited non-zero, surface ExitCode and
		// Output in the error message so the agent sees the compile
		// errors directly.
		var buildErr *verbs.BuildExitError
		if errors.As(err, &buildErr) && buildErr.Result != nil {
			return ErrorResponse(fmt.Sprintf(
				"iris:run_build: %s exited %d\n%s",
				buildErr.Result.Command,
				buildErr.Result.ExitCode,
				buildErr.Result.Output,
			))
		}
		return ErrorResponse(fmt.Sprintf("iris:run_build: %v", err))
	}

	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return ErrorResponse(fmt.Sprintf("iris:run_build: encode result: %v", err))
	}
	return TextResponse(string(body))
}
