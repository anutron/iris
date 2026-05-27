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

func newBranchDeleteRemoteCmd() *cobra.Command {
	var branch string

	cmd := &cobra.Command{
		Use:   "branch-delete-remote <task-id>",
		Short: "Delete a remote branch on origin (direct host invocation)",
		Long:  "Run iris:branch_delete_remote directly against the host shell. Refuses to delete the default branch. Returns the branch's SHA at the time of deletion.",
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

			result, err := verbs.BranchDeleteRemote(cmd.Context(), verbs.BranchDeleteRemoteInput{
				Client: client, TaskID: taskID, Branch: branch,
			})
			if err != nil {
				return err
			}
			body, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(body))
			return nil
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "remote branch to delete (required)")
	_ = cmd.MarkFlagRequired("branch")
	return cmd
}
