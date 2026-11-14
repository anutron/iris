package main

import (
	"encoding/json"
	"fmt"

	"github.com/anutron/iris/internal/verbs"
	"github.com/spf13/cobra"
)

var dogfoodDeployVerb = verbs.DogfoodDeploy

func newDogfoodDeployCmd() *cobra.Command {
	var taskID string
	var opts verbs.DogfoodDeployOpts
	cmd := &cobra.Command{
		Use: "dogfood-deploy", Short: "Build, restart and verify a ref on the dogfood branch",
		Long: "Claim the dogfood lease first. Deploy a reachable ref, or merge it onto the current dogfood tip with --compose. The host stays on dogfood throughout build, restart and readiness. No manual checkout/reload sequence is needed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newCLIClient(cmd.Context())
			if err != nil {
				return fmt.Errorf("argus client required for dogfood-deploy: %w", err)
			}
			opts.Caller = "cli"
			result, err := dogfoodDeployVerb(cmd.Context(), client, taskID, opts)
			if result != nil {
				body, _ := json.MarshalIndent(result, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(body))
			}
			return err
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "argus task ID; omit for self-target")
	cmd.Flags().StringVar(&opts.Ref, "ref", "", "reachable ref or commit to deploy (required)")
	cmd.Flags().BoolVar(&opts.Compose, "compose", false, "merge ref --no-ff onto the current dogfood tip")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "allow a direct deployment to drop existing dogfood commits")
	cmd.Flags().BoolVar(&opts.LeaseOverride, "lease-override", false, "override the dogfood lease gate with a warning")
	_ = cmd.MarkFlagRequired("ref")
	return cmd
}
