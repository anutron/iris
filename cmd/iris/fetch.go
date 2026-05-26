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

func newFetchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fetch <task-id>",
		Short: "Fetch origin in an argus task's source repo (direct host invocation)",
		Long:  "Run iris:fetch directly against the host shell. Same Go function the MCP handler calls; bypasses argus + MCP. Returns the list of refs whose tracking SHAs changed.",
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

			result, err := verbs.Fetch(cmd.Context(), verbs.FetchInput{Client: client, TaskID: taskID})
			if err != nil {
				return err
			}
			body, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(body))
			return nil
		},
	}
}
