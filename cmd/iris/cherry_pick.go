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

func newCherryPickCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cherry-pick <task-id> <commit> <target-branch>",
		Short: "Cherry-pick a commit onto a target branch in the source repo (direct host invocation)",
		Long:  "Run iris:cherry_pick directly against the host shell. Checks out <target-branch> and applies <commit> under the per-source-repo lock; aborts cleanly on conflict. Refuses default branch as target.",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, commit, target := args[0], args[1], args[2]
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

			result, err := verbs.CherryPick(cmd.Context(), verbs.CherryPickInput{
				Client: client, TaskID: taskID, Commit: commit, TargetBranch: target,
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
