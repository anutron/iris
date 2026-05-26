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

func newGHPRMergeCmd() *cobra.Command {
	var (
		prNumber int
		strategy string
	)

	cmd := &cobra.Command{
		Use:   "gh-pr-merge <task-id>",
		Short: "Merge a GitHub PR for an argus task (direct host invocation)",
		Long:  "Run iris:gh_pr_merge directly against the host shell. Shells out to the host's gh CLI; bypasses argus + MCP. v1 does not pre-check CI status; gh's own merge command surfaces a failure when required checks are red.",
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

			result, err := verbs.GHPRMerge(cmd.Context(), client, taskID, verbs.GHPRMergeOptions{
				PRNumber: prNumber,
				Strategy: strategy,
			})
			if err != nil {
				return err
			}
			out, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	cmd.Flags().IntVar(&prNumber, "pr", 0, "PR number to merge (required)")
	cmd.Flags().StringVar(&strategy, "strategy", "squash", "merge strategy: squash | merge | rebase")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}
