package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

// mergeToBranchInput is the wire shape of the MCP tool input.
type mergeToBranchInput struct {
	TaskID       string `json:"task_id"`
	TargetBranch string `json:"target_branch"`
	SourceRef    string `json:"source_ref"`
	NoFF         *bool  `json:"no_ff,omitempty"`
	Message      string `json:"message,omitempty"`
	DryRun       bool   `json:"dry_run,omitempty"`
}

// NewMergeToBranchHandler returns a Handler that decodes the envelope
// input and calls verbs.MergeToBranch.
func NewMergeToBranchHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in mergeToBranchInput
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return ErrorResponse(fmt.Sprintf("iris:merge_to_branch: invalid input: %v", err))
			}
		}
		if in.TaskID == "" {
			return ErrorResponse("iris:merge_to_branch: task_id is required")
		}
		if in.TargetBranch == "" {
			return ErrorResponse("iris:merge_to_branch: target_branch is required")
		}
		if in.SourceRef == "" {
			return ErrorResponse("iris:merge_to_branch: source_ref is required")
		}

		opts := verbs.MergeOptions{NoFF: true, Message: in.Message, DryRun: in.DryRun}
		if in.NoFF != nil {
			opts.NoFF = *in.NoFF
		}

		result, err := verbs.MergeToBranch(ctx, client, in.TaskID, in.TargetBranch, in.SourceRef, opts)
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:merge_to_branch: %v", err))
		}

		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return ErrorResponse(fmt.Sprintf("iris:merge_to_branch: encode result: %v", err))
		}
		return TextResponse(string(body))
	})
}
