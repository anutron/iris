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

func newPublishCmd() *cobra.Command {
	var branch string
	var push bool
	var reset bool

	cmd := &cobra.Command{
		Use:   "publish <task-id>",
		Short: "Update source repo to worktree HEAD, then build + restart",
		Long: `Publish takes the argus task's worktree HEAD and applies it to the source
repo's currently-checked-out branch (ff-only by default; pass --reset for hard
reset), then rebuilds and restarts using the project's .iris.toml. Pass --push
to also push the target branch to origin (subject to the same default-branch
guardrail as iris:push).

v1.2 constraint: the target branch must be the source repo's currently-checked-out
branch. The --branch flag is accepted for forward compatibility but must match.`,
		Args: cobra.ExactArgs(1),
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

			result, err := verbs.Publish(cmd.Context(), client, verbs.PublishInput{
				TaskID: taskID,
				Branch: branch,
				Push:   push,
				Reset:  reset,
				Caller: "cli",
			})
			if result != nil {
				body, _ := json.MarshalIndent(result, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(body))
			}
			return err
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "target branch (defaults to source repo's current branch)")
	cmd.Flags().BoolVar(&push, "push", false, "also push the target branch to origin after the local update")
	cmd.Flags().BoolVar(&reset, "reset", false, "hard-reset the source repo to the worktree HEAD instead of ff-only merge")
	return cmd
}
