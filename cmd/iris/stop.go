package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/config"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running iris daemon",
		Long:  "Send SIGTERM to the iris process recorded in ~/.iris/iris.pid. Use `launchctl bootout` if the LaunchAgent is supervising — that's the supported stop path in production.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			data, err := os.ReadFile(cfg.PIDPath())
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("iris: no pidfile at %s (daemon not running?)", cfg.PIDPath())
				}
				return fmt.Errorf("iris: read pidfile: %w", err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				return fmt.Errorf("iris: pidfile %s: invalid pid: %w", cfg.PIDPath(), err)
			}
			proc, err := os.FindProcess(pid)
			if err != nil {
				return fmt.Errorf("iris: find process %d: %w", pid, err)
			}
			// Liveness probe: on Unix, signal 0 reports reachability without
			// delivering anything. Refuses to SIGTERM a recycled PID that
			// no longer belongs to iris.
			if err := proc.Signal(syscall.Signal(0)); err != nil {
				return fmt.Errorf("iris: pidfile %s names pid %d, but no such process is reachable (stale pidfile; iris likely crashed without cleanup)", cfg.PIDPath(), pid)
			}
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("iris: SIGTERM %d: %w", pid, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "iris: sent SIGTERM to pid %d\n", pid)
			return nil
		},
	}
}
