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

func newBranchCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "branch-create <task-id> <name> <base-ref>",
		Short: "Create a branch in the source repo from an arbitrary ref (direct host invocation)",
		Long:  "Run iris:branch_create directly against the host shell. Creates <name> from <base-ref> without changing the source repo's checkout. Refuses default branch, leading-dash refnames, invalid git refs, and existing branches.",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, name, baseRef := args[0], args[1], args[2]
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

			result, err := verbs.BranchCreate(cmd.Context(), verbs.BranchCreateInput{
				Client: client, TaskID: taskID, Name: name, BaseRef: baseRef,
			})
			if err != nil {
				return err
			}
			body, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(body))
			return nil
		},
	}
}
