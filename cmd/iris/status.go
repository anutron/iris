package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
	"github.com/anutron/iris/internal/verbs"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [target]",
		Short: "Show iris daemon health (no args) or self-management status for a target",
		Long: `Without arguments, prints daemon health: pid, state dir, token file,
argus socket path.

With a target argument, runs the iris:status verb against the resolved
source repo. The target can be:
  - omitted             → iris itself (via os.Executable resolution)
  - prefixed /, ~, .    → filesystem path
  - any other string    → argus task_id

Target form prints the structured result as JSON.`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return runStatusTarget(cmd, args[0])
			}
			return runDaemonHealth(cmd)
		},
	}
}

// runDaemonHealth is the v1.0 daemon-health output, preserved for the
// no-arg call. Self-management consumers pass a target.
func runDaemonHealth(cmd *cobra.Command) error {
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
	data, readErr := os.ReadFile(cfg.TokenPath())
	switch {
	case os.IsNotExist(readErr):
		fmt.Fprintln(cmd.OutOrStdout(), "  (MISSING – run setup.sh)")
	case readErr != nil:
		fmt.Fprintf(cmd.OutOrStdout(), "  (read error: %v)\n", readErr)
	case strings.TrimSpace(string(data)) == "":
		fmt.Fprintln(cmd.OutOrStdout(), "  (EMPTY OR WHITESPACE – re-run setup.sh)")
	default:
		fmt.Fprintln(cmd.OutOrStdout(), "  (present)")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "argus_sock: %s\n", cfg.ArgusSocketPath)
	return nil
}

// runStatusTarget runs the iris:status self-management verb on the
// provided target.
func runStatusTarget(cmd *cobra.Command, target string) error {
	taskID, path := classifyTarget(target)
	client, err := newCLIClient(cmd.Context())
	if err != nil {
		// Allowlist enforcement / argus reachability — surface clearly.
		return err
	}
	result, err := verbs.Status(cmd.Context(), client, verbs.StatusInput{TaskID: taskID, Path: path})
	if err != nil {
		return err
	}
	body, _ := json.MarshalIndent(result, "", "  ")
	fmt.Fprintln(cmd.OutOrStdout(), string(body))
	return nil
}

// classifyTarget translates a CLI positional argument into the (task_id,
// path) pair the resolve dispatcher expects. The rule: arguments starting
// with `/`, `~`, or `.` are filesystem paths; anything else is a task_id.
func classifyTarget(arg string) (taskID, path string) {
	if arg == "" {
		return "", ""
	}
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "~") || strings.HasPrefix(arg, ".") {
		return "", arg
	}
	return arg, ""
}

// newCLIClient builds an argus.Client suitable for direct CLI invocations
// (port discovery via the argus socket + scope token from disk).
func newCLIClient(ctx context.Context) (*argus.Client, error) {
	cfg := config.Default()
	token, err := cfg.LoadToken()
	if err != nil {
		return nil, err
	}
	ports := argus.NewPortsClient(cfg.ArgusSocketPath)
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	apiPort, _, err := ports.Ports(dctx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("argus socket Ports: %w", err)
	}
	return argus.New(fmt.Sprintf("http://127.0.0.1:%d", apiPort), token), nil
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
