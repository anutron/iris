package config

import (
	"strings"
	"syscall"
)

// SignalByName looks up a Unix signal by its standard name (e.g. "SIGTERM").
// The lookup is case-insensitive and accepts the name with or without the
// "SIG" prefix.
func SignalByName(name string) (syscall.Signal, bool) {
	if name == "" {
		return 0, false
	}
	upper := strings.ToUpper(strings.TrimSpace(name))
	if !strings.HasPrefix(upper, "SIG") {
		upper = "SIG" + upper
	}
	sig, ok := signalTable[upper]
	return sig, ok
}

// signalTable enumerates the portable signals iris accepts in the
// `[restart] mechanism = "signal"` block. POSIX-portable signals plus the
// common Unix superset; intentionally narrower than runtime/signal package
// constants to keep iris's `.iris.toml` schema explicit.
var signalTable = map[string]syscall.Signal{
	"SIGHUP":  syscall.SIGHUP,
	"SIGINT":  syscall.SIGINT,
	"SIGQUIT": syscall.SIGQUIT,
	"SIGABRT": syscall.SIGABRT,
	"SIGKILL": syscall.SIGKILL,
	"SIGALRM": syscall.SIGALRM,
	"SIGTERM": syscall.SIGTERM,
	"SIGUSR1": syscall.SIGUSR1,
	"SIGUSR2": syscall.SIGUSR2,
	"SIGCONT": syscall.SIGCONT,
	"SIGSTOP": syscall.SIGSTOP,
	"SIGTSTP": syscall.SIGTSTP,
	"SIGPIPE": syscall.SIGPIPE,
	"SIGWINCH": syscall.SIGWINCH,
	"SIGCHLD": syscall.SIGCHLD,
}
