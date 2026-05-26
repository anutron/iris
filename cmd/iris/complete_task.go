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

func newCompleteTaskCmd() *cobra.Command {
	var mergeStrategy string

	cmd := &cobra.Command{
		Use:   "complete-task <task-id>",
		Short: "Run the composite ship-it sequence on an argus task",
		Long:  "Merge task branch into master, push master to origin, delete remote task branch, mark argus task complete, archive. Returns checkpoints reached so a partial failure can be diagnosed.",
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

			result, err := verbs.CompleteTask(cmd.Context(), client, taskID, verbs.CompleteTaskOptions{MergeStrategy: mergeStrategy})
			if result != nil {
				body, _ := json.MarshalIndent(result, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(body))
			}
			return err
		},
	}
	cmd.Flags().StringVar(&mergeStrategy, "merge-strategy", "no_ff", "merge strategy: no_ff | ff_only")
	return cmd
}
