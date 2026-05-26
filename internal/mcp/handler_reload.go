package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

type reloadInput struct {
	TaskID         string `json:"task_id,omitempty"`
	Path           string `json:"path,omitempty"`
	NoPull         bool   `json:"no_pull,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// NewReloadHandler returns a Handler for iris_reload. Self-vs-cross
// detection happens inside verbs.Reload; the handler is a thin envelope
// translator.
func NewReloadHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in reloadInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:reload: invalid input: %v", err))
			}
		}
		caller := callerFromContext(ctx)
		if caller == "" {
			caller = in.TaskID
			if caller == "" {
				caller = "self"
			}
		}
		result, err := verbs.Reload(ctx, client, verbs.ReloadInput{
			TaskID:         in.TaskID,
			Path:           in.Path,
			NoPull:         in.NoPull,
			TimeoutSeconds: in.TimeoutSeconds,
			Caller:         caller,
		})
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:reload: %v", err))
		}
		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:reload: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}

// callerFromContext returns the argus task_id from the request envelope
// when one is available, or "" if not. The MCP server stores envelope
// context in the request context via this key; tests using nil context
// see "".
type callerKey struct{}

// WithCaller installs the caller identity (typically argus task_id) onto
// ctx for downstream verbs to read.
func WithCaller(ctx context.Context, caller string) context.Context {
	return context.WithValue(ctx, callerKey{}, caller)
}

func callerFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(callerKey{}).(string)
	return v
}
