package argus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// recoverMu serializes Recover() calls across all entry points (watcher
// pid-mtime, watcher ping-failure, registrar heartbeat-404). The spec
// scenario "Recovery is single-flight" promises at most one recovery
// runs concurrently, but iris has multiple independent invokers; the
// watcher's own atomic.Bool gate only covers its own goroutine. This
// mutex provides the cross-invoker guarantee.
var recoverMu sync.Mutex

// Reregistrar is the minimum surface Recover needs from a registrar.
// *mcp.Registrar satisfies this via its ForceReregister method.
//
// Declaring the interface here (in argus) avoids an import cycle —
// argus cannot import mcp because mcp imports argus.
type Reregistrar interface {
	ForceReregister(ctx context.Context) error
}

// RecoverFunc returns a callback suitable for Watcher.OnRestart. The
// returned closure runs the full reconnect sequence on each invocation:
//
//  1. Transition link state to LinkRecovering (and clear LastError).
//  2. Re-query Daemon.Ports over the unix socket.
//  3. Atomically update the shared client's baseURL.
//  4. Force a fresh registration POST through the registrar.
//  5. Transition link state back to LinkHealthy on success, or LinkDown
//     with a wrapped error on any sub-step failure.
//
// Reentrant: the watcher's single-flight gate and the heartbeat-404
// passive fallback may invoke this concurrently. A parallel invocation
// can briefly observe an intermediate LinkRecovering set by the other
// call; benign because both converge on the same target state.
func RecoverFunc(ports *PortsClient, client *Client, mcpReg Reregistrar, log *slog.Logger) func(context.Context) {
	if log == nil {
		log = slog.Default()
	}
	return func(ctx context.Context) {
		Recover(ctx, ports, client, mcpReg, log)
	}
}

// Recover runs one pass of the reconnect sequence described on RecoverFunc.
func Recover(ctx context.Context, ports *PortsClient, client *Client, mcpReg Reregistrar, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}

	// Serialize across all entry points so the spec's "single-flight"
	// guarantee actually holds — even when the watcher AND a heartbeat-404
	// callback fire near-simultaneously.
	recoverMu.Lock()
	defer recoverMu.Unlock()

	SetLinkState(LinkRecovering)
	SetLinkError(nil)
	log.Info("argus link recovering")

	apiPort, _, err := ports.Ports(ctx)
	if err != nil {
		wrapped := fmt.Errorf("ports query: %w", err)
		SetLinkError(wrapped)
		SetLinkState(LinkDown)
		log.Warn("argus link recovery failed", "stage", "ports", "err", err)
		return
	}

	newURL := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	client.SetBaseURL(newURL)

	if err := mcpReg.ForceReregister(ctx); err != nil {
		wrapped := fmt.Errorf("mcp re-register: %w", err)
		SetLinkError(wrapped)
		SetLinkState(LinkDown)
		log.Warn("argus link recovery failed", "stage", "mcp", "err", err)
		return
	}

	SetLinkError(nil)
	SetLinkState(LinkHealthy)
	log.Info("argus link recovered", "argus_base_url", newURL)
}
