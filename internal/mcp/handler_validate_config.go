package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type validateConfigInput struct {
	TaskID string `json:"task_id,omitempty"`
	Path   string `json:"path,omitempty"`
}

// NewValidateConfigHandler returns a Handler for iris_validate_config.
func NewValidateConfigHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in validateConfigInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:validate_config: invalid input: %v", err))
			}
		}
		result, err := verbs.ValidateConfig(ctx, client, verbs.ValidateConfigInput{
			TaskID: in.TaskID,
			Path:   in.Path,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:validate_config: %v", err))
		}
		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:validate_config: encode: %v", err))
		}
		return TextResponse(string(body))
	})
}
