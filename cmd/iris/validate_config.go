package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/verbs"
)

func newValidateConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate-config [target]",
		Short: "Parse and cross-validate a .iris.toml without side effects",
		Long: `Reads .iris.toml at the resolved source repo and reports valid/invalid
with structured errors and remediation hints. No pull, no build, no
restart, no audit-log write.

Target can be:
  - omitted             → iris itself (self)
  - prefixed /, ~, .    → filesystem path
  - any other string    → argus task_id`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var taskID, path string
			if len(args) == 1 {
				taskID, path = classifyTarget(args[0])
			}
			client, _ := newCLIClient(cmd.Context())
			result, err := verbs.ValidateConfig(cmd.Context(), client, verbs.ValidateConfigInput{
				TaskID: taskID, Path: path,
			})
			if err != nil {
				return err
			}
			body, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(body))
			if !result.Valid {
				return fmt.Errorf(".iris.toml is invalid")
			}
			return nil
		},
	}
}
