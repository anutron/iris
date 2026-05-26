package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/config"
	"github.com/anutron/iris/internal/daemon"
)

func newStartCmd() *cobra.Command {
	var foreground bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the iris daemon",
		Long:  "Start the iris daemon. --foreground keeps it in the current shell (useful for dev and what the LaunchAgent calls). Without --foreground, iris exits with a not-yet-implemented error; wrap with nohup or use the LaunchAgent.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()

			if !foreground {
				return fmt.Errorf("iris start without --foreground is not implemented; pass --foreground (or use the LaunchAgent installed by ./setup.sh)")
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			return daemon.Run(ctx, cfg, logger)
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false, "run in the foreground")
	return cmd
}
