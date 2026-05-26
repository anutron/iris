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

func newGHPRViewCmd() *cobra.Command {
	var prNumber int

	cmd := &cobra.Command{
		Use:   "gh-pr-view <task-id>",
		Short: "Read a GitHub PR's state via the host gh CLI (direct host invocation)",
		Long:  "Run iris:gh_pr_view directly against the host shell. Shells out to `gh pr view --json state,checks,reviews,mergeable,headRefName,baseRefName,isDraft,statusCheckRollup`; bypasses argus + MCP.",
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

			result, err := verbs.GHPRView(cmd.Context(), client, taskID, verbs.GHPRViewOptions{PRNumber: prNumber})
			if err != nil {
				return err
			}
			out, _ := json.MarshalIndent(result.Data, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	cmd.Flags().IntVar(&prNumber, "pr", 0, "PR number to view (required)")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}
