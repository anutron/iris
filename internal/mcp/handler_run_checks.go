package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type runChecksInput struct {
	TaskID      string `json:"task_id"`
	Check       string   `json:"check"`
	Args        []string `json:"args,omitempty"`
	WaitSeconds *int     `json:"wait_seconds,omitempty"`
}

// runChecksKey is the duplicate-run key argument: the check plus its args,
// NUL-joined. Args can't contain NUL (ValidateCheckArgs), so the encoding is
// injective: check "a" with args ["b"] never aliases check "a b". Order is
// significant.
func runChecksKey(check string, args []string) string {
	if len(args) == 0 {
		return check
	}
	return check + "\x00" + strings.Join(args, "\x00")
}

// NewRunChecksHandler returns a Handler that decodes the envelope input
// and calls verbs.RunChecks. On a non-zero check exit (the common
// lint/test-failure case), the handler surfaces an error response that
// includes the exit code and the captured output, so the agent sees the
// real rubocop/rspec/brakeman text without a second round-trip.
//
// The check runs as a worktree run in runs: a check still going after
// wait_seconds returns a run_id handle for iris_run_result instead of
// outliving argus's 30s callback limit.
func NewRunChecksHandler(client *argus.Client, runs *RunRegistry) Handler {
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
		// Reject bad args before resolving or registering a run, so a bad
		// call fails now instead of returning a handle that later fails.
		if err := verbs.ValidateCheckArgs(in.Args); err != nil {
			return ErrorResponse(fmt.Sprintf("iris:run_checks: %v", err))
		}

		// Resolve before detaching: bad task_ids fail fast, and the repo is
		// recorded so even an immediate handle names it.
		if _, err := verbs.Resolve(ctx, client, in.TaskID); err != nil {
			return ErrorResponse(fmt.Sprintf("iris:run_checks: %v", err))
		}
		run := runs.Start(ctx, "iris_run_checks", in.TaskID, runChecksKey(in.Check, in.Args), func(ctx context.Context) Response {
			return runChecks(ctx, client, in)
		})
		return runs.Respond(ctx, run, ClampRunWait(in.WaitSeconds))
	})
}

func runChecks(ctx context.Context, client *argus.Client, in runChecksInput) Response {
	result, err := verbs.RunChecks(ctx, client, in.TaskID, verbs.RunChecksOptions{Check: in.Check, Args: in.Args})
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
}
