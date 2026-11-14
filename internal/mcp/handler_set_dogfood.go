package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type setDogfoodInput struct {
	WaitSeconds *int                   `json:"wait_seconds,omitempty"`
	TaskID      string                 `json:"task_id,omitempty"`
	Sha         string                 `json:"sha"`
	Manifest    *verbs.DogfoodManifest `json:"manifest"`
	Force       bool                   `json:"force,omitempty"`
	// LeaseOverride is separate from Force: it bypasses only the dogfood lease.
	LeaseOverride bool `json:"lease_override,omitempty"`
}

// NewSetDogfoodHandler returns a Handler for iris_set_dogfood. The handler is a
// thin envelope translator: config refusal, SHA reachability, manifest
// persistence, branch reset, and reload all happen inside verbs.SetDogfood.
func NewSetDogfoodHandler(client *argus.Client, runs *RunRegistry) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in setDogfoodInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:set_dogfood: invalid input: %v", err))
			}
		}
		if in.Manifest == nil {
			return ErrorResponse("iris:set_dogfood: manifest is required")
		}
		if in.Sha == "" {
			return ErrorResponse("iris:set_dogfood: sha is required")
		}
		args := in
		args.WaitSeconds = nil
		key, _ := json.Marshal(args)
		return deploymentRun(ctx, client, runs, "iris_set_dogfood", in.TaskID, "", string(key), in.WaitSeconds, func(ctx context.Context) Response {
			result, err := verbs.SetDogfood(ctx, client, in.TaskID, verbs.SetDogfoodOpts{Sha: in.Sha, Manifest: in.Manifest, Force: in.Force, LeaseOverride: in.LeaseOverride})
			return deploymentResponse("iris:set_dogfood", result, err)
		})
	})
}
