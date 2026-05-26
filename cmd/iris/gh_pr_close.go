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

func newGHPRCloseCmd() *cobra.Command {
	var (
		prNumber     int
		deleteBranch bool
	)

	cmd := &cobra.Command{
		Use:   "gh-pr-close <task-id>",
		Short: "Close a GitHub PR without merging (direct host invocation)",
		Long:  "Run iris:gh_pr_close directly against the host shell. Shells out to `gh pr close <pr>` (with `--delete-branch` when --delete-branch is set); bypasses argus + MCP.",
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

			result, err := verbs.GHPRClose(cmd.Context(), client, taskID, verbs.GHPRCloseOptions{
				PRNumber:     prNumber,
				DeleteBranch: deleteBranch,
			})
			if err != nil {
				return err
			}
			out, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	cmd.Flags().IntVar(&prNumber, "pr", 0, "PR number to close (required)")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "Delete the source branch after closing the PR (default false)")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}
