package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/verbs"
)

func newSetDogfoodCmd() *cobra.Command {
	var taskID string
	var sha string
	var manifest string
	var force bool
	cmd := &cobra.Command{
		Use:   "set-dogfood",
		Short: "Point the dogfood branch at a SHA, record a manifest, and reload",
		Long: `set-dogfood atomically hard-resets the .iris.toml dogfood_branch to --sha,
persists the --manifest record alongside the audit log, and runs the project's
build/restart machinery.

Iris does not compose: build the SHA yourself (cherry-pick/merge/rebase) and
hand the finished commit here. Refuses any repo whose .iris.toml does not
declare dogfood_branch.

Refuses a --sha that is not a descendant of the current dogfood_branch SHA
(it would silently drop commits) unless --force is passed.

--manifest accepts either a path to a JSON file or an inline JSON string (one
that begins with '{').`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := loadManifestArg(manifest)
			if err != nil {
				return err
			}
			client, err := newCLIClient(cmd.Context())
			if err != nil {
				return fmt.Errorf("argus client required for set-dogfood: %w", err)
			}
			result, err := verbs.SetDogfood(cmd.Context(), client, taskID, verbs.SetDogfoodOpts{
				Sha:      sha,
				Manifest: m,
				Force:    force,
			})
			if result != nil {
				body, _ := json.MarshalIndent(result, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(body))
			}
			return err
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "(optional) argus task ID; omit for self-target")
	cmd.Flags().StringVar(&sha, "sha", "", "full commit SHA to point the dogfood branch at (required)")
	cmd.Flags().StringVar(&manifest, "manifest", "", "manifest as a JSON file path or inline JSON string (required)")
	cmd.Flags().BoolVar(&force, "force", false, "override the commit-dropping ancestry refusal (deploy a --sha that is not a descendant of the current dogfood SHA, dropping commits, with a warning)")
	_ = cmd.MarkFlagRequired("sha")
	_ = cmd.MarkFlagRequired("manifest")
	return cmd
}

// loadManifestArg interprets --manifest as either an inline JSON object (when
// it begins with '{') or a path to a JSON file, then decodes it.
func loadManifestArg(arg string) (*verbs.DogfoodManifest, error) {
	data := []byte(arg)
	if !strings.HasPrefix(strings.TrimSpace(arg), "{") {
		raw, err := os.ReadFile(arg)
		if err != nil {
			return nil, fmt.Errorf("read manifest file %q: %w", arg, err)
		}
		data = raw
	}
	var m verbs.DogfoodManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest JSON: %w", err)
	}
	return &m, nil
}
