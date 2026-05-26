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

func newMergeToMasterCmd() *cobra.Command {
	var noFF bool
	var ffOnly bool
	var message string

	cmd := &cobra.Command{
		Use:   "merge-to-master <task-id>",
		Short: "Merge an argus task's branch into master (direct host invocation)",
		Long:  "Run iris:merge_to_master directly against the host shell. Same Go function the MCP handler calls; bypasses argus + MCP. Useful for debugging.",
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

			opts := verbs.MergeOptions{NoFF: noFF, Message: message}
			if ffOnly {
				opts.NoFF = false
			}

			result, err := verbs.MergeToMaster(cmd.Context(), client, taskID, opts)
			if err != nil {
				return err
			}
			body, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(body))
			return nil
		},
	}
	cmd.Flags().BoolVar(&noFF, "no-ff", true, "pass --no-ff to git merge")
	cmd.Flags().BoolVar(&ffOnly, "ff-only", false, "require fast-forward (overrides --no-ff)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "merge commit message (-m)")
	return cmd
}
