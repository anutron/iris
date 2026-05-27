package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/verbs"
)

func newLsCmd() *cobra.Command {
	var limit int
	var since string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List managed systems iris has reloaded recently",
		Long: `Reads ~/.iris/reload-history.jsonl and projects managed systems with
aggregate counts and last-reload metadata. The audit log is the source
of truth; iris does not maintain a separate registry.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := verbs.Ls(cmd.Context(), verbs.LsInput{Limit: limit, Since: since})
			if err != nil {
				return err
			}
			body, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(body))
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "(optional) maximum number of entries (default 50)")
	cmd.Flags().StringVar(&since, "since", "", "(optional) exclude entries before this RFC3339 timestamp")
	return cmd
}
