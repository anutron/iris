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

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show iris daemon status",
		Long:  "Print whether the daemon is running (from its pidfile), the configured paths, and the scope-token file's presence.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()

			running, pid := pidIsAlive(cfg)
			switch {
			case running:
				fmt.Fprintf(cmd.OutOrStdout(), "iris: running (pid %d)\n", pid)
			case pid != 0:
				fmt.Fprintf(cmd.OutOrStdout(), "iris: stale pidfile (pid %d not alive)\n", pid)
			default:
				fmt.Fprintln(cmd.OutOrStdout(), "iris: not running")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "state_dir:  %s\n", cfg.StateDir)
			fmt.Fprintf(cmd.OutOrStdout(), "token_file: %s", cfg.TokenPath())
			switch _, statErr := os.Stat(cfg.TokenPath()); {
			case os.IsNotExist(statErr):
				fmt.Fprintln(cmd.OutOrStdout(), "  (MISSING – run setup.sh)")
			case statErr != nil:
				fmt.Fprintf(cmd.OutOrStdout(), "  (stat error: %v)\n", statErr)
			default:
				// File exists; LoadToken trims whitespace and rejects
				// empties — surface that distinction to the operator.
				if _, err := cfg.LoadToken(); err != nil {
					fmt.Fprintln(cmd.OutOrStdout(), "  (EMPTY OR WHITESPACE – re-run setup.sh)")
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "  (present)")
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "argus_sock: %s\n", cfg.ArgusSocketPath)
			return nil
		},
	}
}

// pidIsAlive checks whether the recorded PID points at a running process.
func pidIsAlive(cfg *config.Config) (bool, int) {
	data, err := os.ReadFile(cfg.PIDPath())
	if err != nil {
		return false, 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false, 0
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, pid
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false, pid
	}
	return true, pid
}
