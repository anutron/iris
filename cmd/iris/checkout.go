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

func newCheckoutCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "checkout <task-id> <branch>",
		Short: "Switch the source repo to a branch (direct host invocation)",
		Long:  "Run iris:checkout directly against the host shell. With --force, aborts any in-progress merge/cherry-pick/rebase and discards uncommitted changes before switching — the recovery path for a source repo stuck mid-operation.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, branch := args[0], args[1]
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

			result, err := verbs.Checkout(cmd.Context(), verbs.CheckoutInput{
				Client: client, TaskID: taskID, Branch: branch, Force: force,
			})
			if err != nil {
				return err
			}
			body, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(body))
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "abort in-progress merge/cherry-pick/rebase and discard uncommitted changes before switching")
	return cmd
}
