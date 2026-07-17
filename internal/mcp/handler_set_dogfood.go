package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type setDogfoodInput struct {
	TaskID   string                 `json:"task_id,omitempty"`
	Sha      string                 `json:"sha"`
	Manifest *verbs.DogfoodManifest `json:"manifest"`
	Force    bool                   `json:"force,omitempty"`
}

// NewSetDogfoodHandler returns a Handler for iris_set_dogfood. The handler is a
// thin envelope translator: config refusal, SHA reachability, manifest
// persistence, branch reset, and reload all happen inside verbs.SetDogfood.
func NewSetDogfoodHandler(client *argus.Client) Handler {
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
		result, err := verbs.SetDogfood(ctx, client, in.TaskID, verbs.SetDogfoodOpts{
			Sha:      in.Sha,
			Manifest: in.Manifest,
			Force:    in.Force,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:set_dogfood: %v", err))
		}
		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:set_dogfood: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
