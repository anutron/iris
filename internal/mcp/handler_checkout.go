package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type checkoutInput struct {
	TaskID string `json:"task_id"`
	Branch string `json:"branch"`
	Force  bool   `json:"force,omitempty"`
}

// NewCheckoutHandler returns a Handler that decodes the envelope input
// and calls verbs.Checkout.
func NewCheckoutHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in checkoutInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:checkout: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:checkout: task_id is required")
		}
		if in.Branch == "" {
			return ErrorResponse("iris:checkout: branch is required")
		}

		result, err := verbs.Checkout(ctx, verbs.CheckoutInput{
			Client: client, TaskID: in.TaskID, Branch: in.Branch, Force: in.Force,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:checkout: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:checkout: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
