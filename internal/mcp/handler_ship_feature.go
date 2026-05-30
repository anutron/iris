package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type shipFeatureInput struct {
	TaskID      string `json:"task_id,omitempty"`
	Branch      string `json:"branch"`
	Via         string `json:"via"`
	PRTitle     string `json:"pr_title,omitempty"`
	PRBody      string `json:"pr_body,omitempty"`
	MergeMethod string `json:"merge_method,omitempty"`
}

// NewShipFeatureHandler returns a Handler for iris_ship_feature. The handler is
// a thin envelope translator: mode validation, branch checks, push, and PR
// creation all happen inside verbs.ShipFeature.
func NewShipFeatureHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in shipFeatureInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:ship_feature: invalid input: %v", err))
			}
		}
		result, err := verbs.ShipFeature(ctx, client, in.TaskID, verbs.ShipFeatureOpts{
			Branch:      in.Branch,
			Via:         in.Via,
			PRTitle:     in.PRTitle,
			PRBody:      in.PRBody,
			MergeMethod: in.MergeMethod,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:ship_feature: %v", err))
		}
		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:ship_feature: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
