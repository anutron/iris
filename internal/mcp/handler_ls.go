package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/verbs"
)

type lsInput struct {
	Limit int    `json:"limit,omitempty"`
	Since string `json:"since,omitempty"`
}

// NewLsHandler returns a Handler for iris_ls. The handler does not need
// the argus client; the audit log is the source of truth.
func NewLsHandler() Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in lsInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:ls: invalid input: %v", err))
			}
		}
		result, err := verbs.Ls(ctx, verbs.LsInput{Limit: in.Limit, Since: in.Since})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:ls: %v", err))
		}
		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:ls: encode: %v", err))
		}
		return TextResponse(string(body))
	})
}
