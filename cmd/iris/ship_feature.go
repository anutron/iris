package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/verbs"
)

func newShipFeatureCmd() *cobra.Command {
	var taskID string
	var branch string
	var via string
	var title string
	var body string
	var mergeMethod string
	cmd := &cobra.Command{
		Use:   "ship-feature",
		Short: "Ship a feature branch to origin's default branch via a GitHub PR",
		Long: `ship-feature lands a feature branch on origin's default branch through a
GitHub pull request.

--via pr pushes the branch and opens a PR targeting the default branch, then
stops; the worker returns to it after review. pr mode never merges, fetches, or
touches the dogfood branch.

--via pr-auto goes further: it waits for the PR's required CI checks to pass,
then approves, merges (using --merge-method), and fetches origin. If CI fails or
times out, the PR is left open and the command reports shipped=false with a
ci_failed / ci_timeout warning.

--title defaults to the branch's last commit subject when omitted.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newCLIClient(cmd.Context())
			if err != nil {
				return fmt.Errorf("argus client required for ship-feature: %w", err)
			}
			result, err := verbs.ShipFeature(cmd.Context(), client, taskID, verbs.ShipFeatureOpts{
				Branch:      branch,
				Via:         via,
				PRTitle:     title,
				PRBody:      body,
				MergeMethod: mergeMethod,
			})
			if result != nil {
				out, _ := json.MarshalIndent(result, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
			}
			return err
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "(optional) argus task ID; omit for self-target")
	cmd.Flags().StringVar(&branch, "branch", "", "feature branch to ship (required)")
	cmd.Flags().StringVar(&via, "via", "", "ship mode: pr or pr-auto (required)")
	cmd.Flags().StringVar(&title, "title", "", "PR title (defaults to the branch's last commit subject)")
	cmd.Flags().StringVar(&body, "body", "", "PR body")
	cmd.Flags().StringVar(&mergeMethod, "merge-method", "squash", "merge method: squash, merge, or rebase (pr-auto only)")
	_ = cmd.MarkFlagRequired("branch")
	_ = cmd.MarkFlagRequired("via")
	return cmd
}
