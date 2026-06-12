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

func newRunChecksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run-checks <task-id> <check>",
		Short: "Run a repo-defined check for an argus task (direct host invocation)",
		Long: `Run iris:run_checks directly against the host shell. Same Go function the MCP
handler calls; bypasses argus + MCP. Runs the repo's check script:
  script/iris-check <check> (executable) in the worktree

Unlike run-build there is no Makefile fallback — checks are script-only. If
script/iris-check is absent or non-executable, this errors with an actionable
message. The check runs in the worktree, not the source repo.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
			check := args[1]

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

			result, err := verbs.RunChecks(cmd.Context(), client, taskID, verbs.RunChecksOptions{Check: check})
			if result != nil {
				// Even on non-zero exit, print the structured result so the
				// caller sees the check output.
				body, _ := json.MarshalIndent(result, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(body))
			}
			if err != nil {
				var checkErr *verbs.CheckExitError
				if errors.As(err, &checkErr) {
					// Check ran but failed; cobra exits non-zero via the
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
