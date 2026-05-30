package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/verbs"
)

// newSetLocalConfigCmd wires `iris set-local-config` to verbs.SetLocalConfig.
//
// --field is repeatable: each occurrence is a name=value pair. The CLI tries
// to parse the value as an integer first (matching how a TOML reader would
// see "900") and falls back to a string. The verb's per-field validator
// catches any type mismatch.
//
// --delete is repeatable: each occurrence names one local field to remove.
func newSetLocalConfigCmd() *cobra.Command {
	var taskID string
	var fieldArgs []string
	var deleteArgs []string

	cmd := &cobra.Command{
		Use:   "set-local-config",
		Short: "Write per-developer fields into .iris.local.toml at the source repo root",
		Long: `set-local-config writes (or merges into) .iris.local.toml at the resolved source
repo's root. It accepts only local-tagged fields (dogfood_branch,
ship_ci_timeout_seconds); shared fields are refused with a field_not_local
error pointing at .iris.toml.

Repeat --field name=value for each field to set:

  iris set-local-config --field dogfood_branch=dev --field ship_ci_timeout_seconds=900

Repeat --delete name to remove a field. Missing names are silently ignored:

  iris set-local-config --delete ship_ci_timeout_seconds

Combine both in one call:

  iris set-local-config --field dogfood_branch=scratch --delete ship_ci_timeout_seconds

The file is rewritten atomically (tmp + rename). The verb does NOT trigger a
reload — the new values take effect on the next reload/status.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fields, err := parseFieldArgs(fieldArgs)
			if err != nil {
				return err
			}
			client, err := newCLIClient(cmd.Context())
			if err != nil {
				return fmt.Errorf("argus client required for set-local-config: %w", err)
			}
			result, err := verbs.SetLocalConfig(cmd.Context(), client, taskID, verbs.SetLocalConfigOpts{
				Fields: fields,
				Delete: deleteArgs,
			})
			if result != nil {
				body, _ := json.MarshalIndent(result, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(body))
			}
			if err != nil {
				// Render structured errors in the same {code, field, message?, hint}
				// shape MCP clients see, so the CLI is a faithful mirror.
				var sErr *verbs.SetLocalConfigError
				if errors.As(err, &sErr) {
					envelope := map[string]string{}
					envelope["code"] = sErr.Code
					if sErr.Field != "" {
						envelope["field"] = sErr.Field
					}
					if sErr.Message != "" {
						envelope["message"] = sErr.Message
					}
					if sErr.Hint != "" {
						envelope["hint"] = sErr.Hint
					}
					body, _ := json.MarshalIndent(envelope, "", "  ")
					fmt.Fprintln(cmd.ErrOrStderr(), string(body))
				}
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&taskID, "task", "", "(optional) argus task ID; omit for self-target")
	cmd.Flags().StringArrayVar(&fieldArgs, "field", nil, "field to set as name=value; repeat to set multiple")
	cmd.Flags().StringArrayVar(&deleteArgs, "delete", nil, "field name to remove; repeat to delete multiple")
	return cmd
}

// parseFieldArgs splits each "name=value" arg into a map[string]any entry.
// Values that parse cleanly as int64 are stored as int64 so the verb's
// per-field validator sees the same type a TOML loader would see; everything
// else stays a string.
func parseFieldArgs(args []string) (map[string]any, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := map[string]any{}
	for _, a := range args {
		idx := strings.IndexByte(a, '=')
		if idx <= 0 {
			return nil, fmt.Errorf("invalid --field %q: expected name=value", a)
		}
		name := a[:idx]
		raw := a[idx+1:]
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			out[name] = n
			continue
		}
		out[name] = raw
	}
	return out, nil
}
