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

func newTagCmd() *cobra.Command {
	var (
		tag     string
		message string
	)

	cmd := &cobra.Command{
		Use:   "tag <task-id>",
		Short: "Create an annotated tag at origin/<default-branch> and push it (direct host invocation)",
		Long:  "Run iris:tag directly against the host shell. Creates an annotated tag at the SHA of origin/<default-branch> and pushes it to origin. Refuses if the tag already exists locally or on origin.",
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

			result, err := verbs.Tag(cmd.Context(), verbs.TagInput{
				Client: client, TaskID: taskID, Tag: tag, Message: message,
			})
			if err != nil {
				return err
			}
			body, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(body))
			return nil
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "tag name (required)")
	cmd.Flags().StringVar(&message, "message", "", "annotation message (default \"Released by iris\")")
	_ = cmd.MarkFlagRequired("tag")
	return cmd
}
