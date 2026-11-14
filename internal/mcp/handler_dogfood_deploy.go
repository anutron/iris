package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type dogfoodDeployInput struct {
	TaskID        string `json:"task_id,omitempty"`
	Ref           string `json:"ref"`
	Compose       bool   `json:"compose,omitempty"`
	Force         bool   `json:"force,omitempty"`
	LeaseOverride bool   `json:"lease_override,omitempty"`
	WaitSeconds   *int   `json:"wait_seconds,omitempty"`
}

// NewDogfoodDeployHandler returns a bounded, pollable host deploy operation.
func NewDogfoodDeployHandler(client *argus.Client, runs *RunRegistry) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in dogfoodDeployInput
		if r := decodeInput("iris:dogfood_deploy", input, &in); r != nil {
			return *r
		}
		if in.Ref == "" {
			return ErrorResponse("iris:dogfood_deploy: ref is required")
		}
		args := in
		args.WaitSeconds = nil
		key, _ := json.Marshal(args)
		return deploymentRun(ctx, client, runs, "iris_dogfood_deploy", in.TaskID, "", string(key), in.WaitSeconds, func(ctx context.Context) Response {
			result, err := verbs.DogfoodDeploy(ctx, client, in.TaskID, verbs.DogfoodDeployOpts{Ref: in.Ref, Compose: in.Compose, Force: in.Force, LeaseOverride: in.LeaseOverride})
			return deploymentResponse("iris:dogfood_deploy", result, err)
		})
	})
}

func deploymentRun(ctx context.Context, client *argus.Client, runs *RunRegistry, tool, taskID, path, key string, wait *int, fn func(context.Context) Response) Response {
	// Resolve while still attached, so invalid targets fail immediately and
	// even an immediate handle records the target repository.
	if _, err := verbs.ResolveTarget(ctx, client, taskID, path); err != nil {
		return ErrorResponse(fmt.Sprintf("%s: %v", tool, err))
	}
	run := runs.Start(ctx, tool, taskID, key, fn)
	return runs.Respond(ctx, run, ClampRunWait(wait))
}

func deploymentResponse(tool string, result any, err error) Response {
	if err != nil {
		// Typed nil pointers marshal as null. Only attach actual partial results.
		raw, _ := json.Marshal(result)
		if string(raw) != "null" {
			return ErrorResponseWithFields(fmt.Sprintf("%s: %v", tool, err), result)
		}
		return leaseErr(tool, err)
	}
	return leaseJSON(tool, result)
}
