package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

// mergeToMasterInput is the wire shape of the MCP tool input.
type mergeToMasterInput struct {
	TaskID  string `json:"task_id"`
	NoFF    *bool  `json:"no_ff,omitempty"`
	Message string `json:"message,omitempty"`
}

// NewMergeToMasterHandler returns a Handler that decodes the envelope
// input and calls verbs.MergeToMaster.
func NewMergeToMasterHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in mergeToMasterInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:merge_to_master: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:merge_to_master: task_id is required")
		}

		opts := verbs.MergeOptions{NoFF: true, Message: in.Message}
		if in.NoFF != nil {
			opts.NoFF = *in.NoFF
		}

		result, err := verbs.MergeToMaster(ctx, client, in.TaskID, opts)
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:merge_to_master: %v", err))
		}

		// Don't echo the source-repo absolute path back to the sandboxed
		// agent. The agent supplied a task_id; iris resolved the path
		// internally; the path leaves iris only via the daemon log file.
		response := struct {
			SHA           string `json:"sha"`
			DefaultBranch string `json:"default_branch"`
			TaskBranch    string `json:"task_branch"`
			Log           string `json:"log"`
		}{
			SHA:           result.SHA,
			DefaultBranch: result.DefaultBranch,
			TaskBranch:    result.TaskBranch,
			Log:           result.Log,
		}
		body, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:merge_to_master: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
