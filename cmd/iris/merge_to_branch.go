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

func newMergeToBranchCmd() *cobra.Command {
	var noFF bool
	var ffOnly bool
	var message string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "merge-to-branch <task-id> <target-branch> <source-ref>",
		Short: "Merge an arbitrary source_ref into an arbitrary target_branch via a scratch worktree (direct host invocation)",
		Long:  "Run iris:merge_to_branch directly against the host shell. Same Go function the MCP handler calls; bypasses argus + MCP. Merges in a scratch `git worktree add` so the source repo's current checkout is never touched. Refuses the default/protected branch as target_branch (use merge-to-master for that).",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, targetBranch, sourceRef := args[0], args[1], args[2]
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

			opts := verbs.MergeOptions{NoFF: noFF, Message: message, DryRun: dryRun}
			if ffOnly {
				opts.NoFF = false
			}

			result, err := verbs.MergeToBranch(cmd.Context(), client, taskID, targetBranch, sourceRef, opts)
			if err != nil {
				return err
			}
			body, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(body))
			return nil
		},
	}
	cmd.Flags().BoolVar(&noFF, "no-ff", true, "pass --no-ff to git merge")
	cmd.Flags().BoolVar(&ffOnly, "ff-only", false, "require fast-forward (cannot be combined with --no-ff)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "merge commit message (-m)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview the merge: run `git merge --no-commit --no-ff` in the scratch worktree, capture would-be state, abort cleanly; no push, no post_merge hook")
	cmd.MarkFlagsMutuallyExclusive("no-ff", "ff-only")
	return cmd
}
