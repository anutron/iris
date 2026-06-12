// Command iris is the argus plugin that performs allowlisted host-side
// operations on behalf of sandboxed agents. See README.md and SKETCH.md.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is the iris binary version. Set via -ldflags at build time:
//
//	go build -ldflags "-X main.Version=v0.1.0"
var Version = "dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "iris",
		Short:         "Argus plugin for typed host-side operations",
		Long:          "iris is an argus plugin that exposes a fixed allowlist of typed verbs (merge_to_master, push, gh_pr_create, run_build, …) to sandboxed agents over the argus plugin contract. Each verb is a Go function with typed arguments; there is no generic command passthrough.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newStartCmd())
	root.AddCommand(newStopCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newMergeToMasterCmd())
	root.AddCommand(newPushCmd())
	root.AddCommand(newGHPRCreateCmd())
	root.AddCommand(newGHPRMergeCmd())
	root.AddCommand(newGHPRViewCmd())
	root.AddCommand(newGHPRReadyCmd())
	root.AddCommand(newGHPRCommentCmd())
	root.AddCommand(newGHPRCloseCmd())
	root.AddCommand(newRunBuildCmd())
	root.AddCommand(newRunChecksCmd())
	root.AddCommand(newCompleteTaskCmd())
	root.AddCommand(newFetchCmd())
	root.AddCommand(newBranchDeleteRemoteCmd())
	root.AddCommand(newBranchCreateCmd())
	root.AddCommand(newCherryPickCmd())
	root.AddCommand(newCheckoutCmd())
	root.AddCommand(newTagCmd())
	root.AddCommand(newReloadCmd())
	root.AddCommand(newSetDogfoodCmd())
	root.AddCommand(newSetLocalConfigCmd())
	root.AddCommand(newShipFeatureCmd())
	root.AddCommand(newPublishCmd())
	root.AddCommand(newValidateConfigCmd())
	root.AddCommand(newLsCmd())
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "iris: %v\n", err)
		os.Exit(1)
	}
}
