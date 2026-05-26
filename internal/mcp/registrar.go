package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// DefaultHeartbeat is how often Registrar re-POSTs each tool registration.
// Argus's idle sweep defaults to 10 minutes; re-registering at half that
// keeps a comfortable margin.
const DefaultHeartbeat = 5 * time.Minute

// ToolDefinition is one tool iris wants to register with argus. Name MUST
// be prefixed `iris_` (argus enforces scope-prefix matching on register).
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// Registrar owns the lifecycle of iris's tool registrations with argus:
// register on start, heartbeat on a ticker, unregister on stop.
type Registrar struct {
	client      *argus.Client
	callbackURL string
	authHeader  string
	log         *slog.Logger

	mu        sync.Mutex
	heartbeat time.Duration
	tools     []ToolDefinition
	stop      chan struct{}
	wg        sync.WaitGroup

	// onHeartbeat404 is invoked when the heartbeat re-register POST
	// returns 404 — argus garbage-collected the registration (typically
	// because the daemon restarted). The daemon wires this to
	// argus.RecoverFunc as a passive fallback for the rare case where
	// the Watcher missed the argus restart signal.
	onHeartbeat404 func(context.Context)
}

// NewRegistrar constructs a Registrar. callbackBaseURL is the URL prefix
// iris's MCP server hosts (e.g. "http://127.0.0.1:43217"); the per-tool
// URL becomes "<callbackBaseURL>/mcp/<tool-name>".
func NewRegistrar(client *argus.Client, callbackBaseURL, authHeader string, log *slog.Logger) *Registrar {
	if log == nil {
		log = slog.Default()
	}
	return &Registrar{
		client:      client,
		callbackURL: callbackBaseURL,
		authHeader:  authHeader,
		log:         log,
		heartbeat:   DefaultHeartbeat,
	}
}

// SetHeartbeat overrides the default heartbeat duration. Useful in tests.
func (r *Registrar) SetHeartbeat(d time.Duration) {
	r.mu.Lock()
	r.heartbeat = d
	r.mu.Unlock()
}

// SetOnHeartbeat404 registers a callback fired when the heartbeat re-POST
// observes a 404 response from argus. The daemon binds this to
// argus.RecoverFunc as a passive recovery fallback (the Watcher is the
// fast path; the heartbeat catches the rare miss).
func (r *Registrar) SetOnHeartbeat404(fn func(context.Context)) {
	r.mu.Lock()
	r.onHeartbeat404 = fn
	r.mu.Unlock()
}

// ForceReregister POSTs every registered tool to argus immediately,
// bypassing the heartbeat ticker. The argus-link recovery routine calls
// this after argus restarts so the new daemon's tool catalog is
// repopulated without waiting up to one heartbeat.
func (r *Registrar) ForceReregister(ctx context.Context) error {
	return r.registerAll(ctx)
}

// Add records a tool that should be registered when Start is called.
// Adding after Start is safe; the next heartbeat will register the new tool.
func (r *Registrar) Add(def ToolDefinition) {
	r.mu.Lock()
	r.tools = append(r.tools, def)
	r.mu.Unlock()
}

// Tools returns the list of tools the registrar is managing.
func (r *Registrar) Tools() []ToolDefinition {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ToolDefinition, len(r.tools))
	copy(out, r.tools)
	return out
}

// Start performs the initial registration and launches the heartbeat
// goroutine. Returns when initial registration completes.
func (r *Registrar) Start(ctx context.Context) error {
	if err := r.registerAll(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	r.stop = make(chan struct{})
	stop := r.stop
	hb := r.heartbeat
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(hb)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if err := r.registerAll(ctx); err != nil {
					r.log.Warn("heartbeat re-register failed", "err", err)
					var httpErr *argus.HTTPError
					if errors.As(err, &httpErr) && httpErr.StatusCode == 404 {
						r.mu.Lock()
						cb := r.onHeartbeat404
						r.mu.Unlock()
						if cb != nil {
							cb(ctx)
						}
					}
				}
			}
		}
	}()
	return nil
}

// Stop halts the heartbeat goroutine, waits for any in-flight re-register
// to complete, then DELETEs every registered tool from argus.
func (r *Registrar) Stop(ctx context.Context) error {
	r.mu.Lock()
	if r.stop != nil {
		close(r.stop)
		r.stop = nil
	}
	tools := append([]ToolDefinition(nil), r.tools...)
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}

	var firstErr error
	for _, t := range tools {
		if err := r.client.UnregisterTool(ctx, t.Name); err != nil {
			r.log.Warn("unregister failed", "tool", t.Name, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			r.log.Info("unregistered", "tool", t.Name)
		}
	}
	return firstErr
}

// registerAll POSTs every tool registration to argus. Idempotent on the
// argus side (re-POST refreshes the heartbeat).
func (r *Registrar) registerAll(ctx context.Context) error {
	r.mu.Lock()
	tools := append([]ToolDefinition(nil), r.tools...)
	r.mu.Unlock()

	for _, t := range tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		_, err := r.client.RegisterTool(ctx, argus.MCPTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
			CallbackURL: r.callbackURL + "/mcp/" + t.Name,
			AuthHeader:  r.authHeader,
		})
		if err != nil {
			return fmt.Errorf("register %q: %w", t.Name, err)
		}
		r.log.Debug("registered", "tool", t.Name)
	}
	return nil
}
