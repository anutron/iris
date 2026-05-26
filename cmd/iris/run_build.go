package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
	"github.com/anutron/iris/internal/verbs"
)

func newRunBuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run-build <task-id> [target]",
		Short: "Run the project's build for an argus task (direct host invocation)",
		Long: `Run iris:run_build directly against the host shell. Same Go function the MCP
handler calls; bypasses argus + MCP. Discovers the build command by convention:
  1. script/iris-build (executable) in the worktree
  2. Makefile (build target) in the worktree
  3. Otherwise: error with actionable message.

The build runs in the worktree, not the source repo.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
			var target string
			if len(args) == 2 {
				target = args[1]
			}

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

			result, err := verbs.RunBuild(cmd.Context(), client, taskID, verbs.RunBuildOptions{Target: target})
			if result != nil {
				// Even on non-zero exit, print the structured result so the
				// caller sees the build output.
				body, _ := json.MarshalIndent(result, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(body))
			}
			if err != nil {
				var buildErr *verbs.BuildExitError
				if errors.As(err, &buildErr) {
					// Build ran but failed; cobra exits non-zero via the
					// returned error, and we've already printed the result.
					return err
				}
				return err
			}
			return nil
		},
	}
	return cmd
}
