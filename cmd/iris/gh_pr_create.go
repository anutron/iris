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

func newGHPRCreateCmd() *cobra.Command {
	var (
		title    string
		body     string
		draft    bool
		head     string
		baseRepo string
	)

	cmd := &cobra.Command{
		Use:   "gh-pr-create <task-id>",
		Short: "Open a GitHub PR for an argus task's branch (direct host invocation)",
		Long:  "Run iris:gh_pr_create directly against the host shell. Shells out to the host's gh CLI; bypasses argus + MCP. Refuses to open a PR from the default branch.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
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

			result, err := verbs.GHPRCreate(cmd.Context(), client, taskID, verbs.GHPRCreateOptions{
				Title:    title,
				Body:     body,
				Draft:    draft,
				Head:     head,
				BaseRepo: baseRepo,
			})
			if err != nil {
				return err
			}
			out, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "PR title (required)")
	cmd.Flags().StringVar(&body, "body", "", "PR body (optional; gh's default applies when empty)")
	cmd.Flags().BoolVar(&draft, "draft", false, "Open the PR as a draft")
	cmd.Flags().StringVar(&head, "head", "", "(optional) head branch to open the PR for instead of the task's resolved branch. MUST NOT be the default branch.")
	cmd.Flags().StringVar(&baseRepo, "base-repo", "", "(optional) open a same-repo PR on this owner/repo (e.g. drn/argus) instead of auto-detecting a fork.")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}
