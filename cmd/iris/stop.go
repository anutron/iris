package main

import (
	"fmt"
	"os"
	"os/exec"
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
			// Liveness probe: signal 0 reports reachability without
			// delivering anything. Refuses to SIGTERM a dead PID.
			if err := proc.Signal(syscall.Signal(0)); err != nil {
				return fmt.Errorf("iris: pidfile %s names pid %d, but no such process is reachable (stale pidfile; iris likely crashed without cleanup)", cfg.PIDPath(), pid)
			}
			// Defense in depth: confirm the live process is actually iris,
			// not a recycled PID that some other process now owns.
			if comm, err := processName(pid); err != nil {
				return fmt.Errorf("iris: cannot verify pid %d belongs to iris: %w", pid, err)
			} else if comm != "iris" {
				return fmt.Errorf("iris: pid %d is %q, not iris (recycled PID; pidfile %s is stale)", pid, comm, cfg.PIDPath())
			}
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("iris: SIGTERM %d: %w", pid, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "iris: sent SIGTERM to pid %d\n", pid)
			return nil
		},
	}
}

// processName returns the process name (argv[0] basename) for pid on
// macOS/Linux via `ps -o comm= -p <pid>`. Returns the trimmed value.
func processName(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", fmt.Errorf("ps: %w", err)
	}
	// `ps -o comm=` returns the full path on macOS; take the basename.
	raw := strings.TrimSpace(string(out))
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		raw = raw[i+1:]
	}
	return raw, nil
}
