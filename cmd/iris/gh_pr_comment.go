package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
	"github.com/anutron/iris/internal/verbs"
)

func newGHPRCommentCmd() *cobra.Command {
	var (
		prNumber int
		body     string
	)

	cmd := &cobra.Command{
		Use:   "gh-pr-comment <task-id>",
		Short: "Post a comment to a GitHub PR (direct host invocation)",
		Long:  "Run iris:gh_pr_comment directly against the host shell. Shells out to `gh pr comment <pr> --body <body>`; bypasses argus + MCP. Refuses empty/whitespace bodies.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
			cfg := config.Default()
			token, err := cfg.LoadToken()
			if err != nil {
				return err
			}

			ports := argus.NewPortsClient(cfg.ArgusSocketPath)
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			apiPort, _, err := ports.Ports(ctx)
			cancel()
			if err != nil {
				return fmt.Errorf("iris: argus socket Ports: %w", err)
			}
			client := argus.New(fmt.Sprintf("http://127.0.0.1:%d", apiPort), token)

			result, err := verbs.GHPRComment(cmd.Context(), client, taskID, verbs.GHPRCommentOptions{
				PRNumber: prNumber,
				Body:     body,
			})
			if err != nil {
				return err
			}
			out, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	cmd.Flags().IntVar(&prNumber, "pr", 0, "PR number to comment on (required)")
	cmd.Flags().StringVar(&body, "body", "", "Comment body (required, non-empty)")
	_ = cmd.MarkFlagRequired("pr")
	_ = cmd.MarkFlagRequired("body")
	return cmd
}
