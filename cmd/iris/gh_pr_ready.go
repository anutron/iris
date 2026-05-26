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

func newGHPRReadyCmd() *cobra.Command {
	var prNumber int

	cmd := &cobra.Command{
		Use:   "gh-pr-ready <task-id>",
		Short: "Mark a draft GitHub PR as ready for review (direct host invocation)",
		Long:  "Run iris:gh_pr_ready directly against the host shell. Pre-fetches isDraft so the result reports whether the call actually moved state; idempotent if the PR is already ready.",
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

			result, err := verbs.GHPRReady(cmd.Context(), client, taskID, verbs.GHPRReadyOptions{PRNumber: prNumber})
			if err != nil {
				return err
			}
			out, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	cmd.Flags().IntVar(&prNumber, "pr", 0, "PR number to mark ready (required)")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}
