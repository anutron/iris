package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

// setLocalConfigInput is the JSON shape the MCP handler accepts. `fields`
// is decoded as map[string]any so callers may send the raw types they want
// stored (string for dogfood_branch, int for ship_ci_timeout_seconds). The
// verb's per-field validator decides what is acceptable.
type setLocalConfigInput struct {
	TaskID string         `json:"task_id,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
	Delete []string       `json:"delete,omitempty"`
}

// setLocalConfigErrorEnvelope is the JSON shape surfaced when the verb
// refuses an input. Spec scenarios assert the {code, field, message?, hint}
// structure, so we render it explicitly rather than letting the default
// ErrorResponse stringify the SetLocalConfigError.
type setLocalConfigErrorEnvelope struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

// NewSetLocalConfigHandler returns a Handler for iris_set_local_config.
//
// The handler is a thin envelope translator: every refusal flows through
// verbs.SetLocalConfig as a *SetLocalConfigError, which we unwrap into the
// documented JSON shape so MCP clients can branch on `code` without
// regex-matching prose.
func NewSetLocalConfigHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in setLocalConfigInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:set_local_config: invalid input: %v", err))
			}
		}
		result, err := verbs.SetLocalConfig(ctx, client, in.TaskID, verbs.SetLocalConfigOpts{
			Fields: in.Fields,
			Delete: in.Delete,
		})
		if err != nil {
			var sErr *verbs.SetLocalConfigError
			if errors.As(err, &sErr) {
				body, mErr := json.MarshalIndent(setLocalConfigErrorEnvelope{
					Code:    sErr.Code,
					Field:   sErr.Field,
					Message: sErr.Message,
					Hint:    sErr.Hint,
				}, "", "  ")
				if mErr != nil {
					return ErrorResponse(fmt.Sprintf("iris:set_local_config: %v", err))
				}
				return ErrorResponse(string(body))
			}
			return ErrorResponse(fmt.Sprintf("iris:set_local_config: %v", err))
		}
		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:set_local_config: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
