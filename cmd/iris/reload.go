package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/verbs"
)

func newReloadCmd() *cobra.Command {
	var noPull bool
	var timeoutSeconds int
	cmd := &cobra.Command{
		Use:   "reload [target]",
		Short: "Live-upgrade an iris-managed daemon",
		Long: `Reload pulls the default branch, runs the project's build, and dispatches
to the project-declared restart mechanism in .iris.toml.

Target can be:
  - omitted             → iris itself (self-reload)
  - prefixed /, ~, .    → filesystem path
  - any other string    → argus task_id`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var taskID, path string
			if len(args) == 1 {
				taskID, path = classifyTarget(args[0])
			}
			client, err := newCLIClient(cmd.Context())
			// For pure-self reloads, the argus client may not even be needed,
			// but we still build it so allowlist enforcement works when the
			// caller passes a target.
			if err != nil {
				// Don't fail here if the call is self-target only; verbs.Reload
				// will pass nil through safely.
				client = nil
			}
			result, err := verbs.Reload(cmd.Context(), client, verbs.ReloadInput{
				TaskID:         taskID,
				Path:           path,
				NoPull:         noPull,
				TimeoutSeconds: timeoutSeconds,
				Caller:         "cli",
			})
			if result != nil {
				body, _ := json.MarshalIndent(result, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(body))
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&noPull, "no-pull", false, "skip the git fetch/ff-merge and build from current HEAD")
	cmd.Flags().IntVar(&timeoutSeconds, "timeout", 0, "(optional) per-step timeout override in seconds")
	return cmd
}
