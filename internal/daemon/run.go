// Package daemon assembles iris's runtime: argus client, MCP server,
// registrar, verbs handlers. The exported Run() is what `iris start
// --foreground` invokes.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
	"github.com/anutron/iris/internal/mcp"
)

// Daemon bundles every running component so callers (tests) can poke at
// individual subsystems. Production callers use Run() and forget.
type Daemon struct {
	Cfg       *config.Config
	Log       *slog.Logger
	Argus     *argus.Client
	Ports     *argus.PortsClient
	MCPServer *mcp.Server
	Registrar *mcp.Registrar
}

// Start brings every subsystem up and returns the live Daemon.
func Start(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Daemon, error) {
	if cfg == nil {
		cfg = config.Default()
	}
	if log == nil {
		log = slog.Default()
	}

	if err := cfg.EnsureStateDir(); err != nil {
		return nil, fmt.Errorf("iris: state dir: %w", err)
	}
	token, err := cfg.LoadToken()
	if err != nil {
		return nil, err
	}

	// Discover argus's REST port via the local socket. A failure here is
	// fatal: iris cannot operate without argus.
	ports := argus.NewPortsClient(cfg.ArgusSocketPath)
	discoverCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	apiPort, _, err := ports.Ports(discoverCtx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("iris: argus socket Ports: %w", err)
	}
	argusBaseURL := fmt.Sprintf("http://127.0.0.1:%d", apiPort)

	client := argus.New(argusBaseURL, token)

	auth, err := mcp.GenerateAuthHeader()
	if err != nil {
		return nil, fmt.Errorf("iris: generate mcp auth: %w", err)
	}

	mcpSrv := mcp.NewServer(cfg.ListenAddr, auth, log)
	if err := mcpSrv.Start(ctx); err != nil {
		return nil, fmt.Errorf("iris: start mcp server: %w", err)
	}

	// Register handlers for every verb iris exposes.
	mcpSrv.RegisterHandler("iris_merge_to_master", mcp.NewMergeToMasterHandler(client))

	registrar := mcp.NewRegistrar(client, mcpSrv.CallbackBaseURL(), auth, log)
	registrar.SetHeartbeat(cfg.MCPHeartbeat)
	for _, def := range toolDefinitions() {
		registrar.Add(def)
	}
	if err := registrar.Start(ctx); err != nil {
		_ = mcpSrv.Stop()
		return nil, fmt.Errorf("iris: register tools: %w", err)
	}

	log.Info("iris ready",
		"argus_base_url", argusBaseURL,
		"mcp_addr", mcpSrv.Addr(),
		"state_dir", cfg.StateDir,
	)

	return &Daemon{
		Cfg: cfg, Log: log, Argus: client, Ports: ports,
		MCPServer: mcpSrv, Registrar: registrar,
	}, nil
}

// Stop gracefully shuts every subsystem down. Bounded by a 10s deadline
// on the unregister loop so a stuck argus doesn't block shutdown.
func (d *Daemon) Stop(ctx context.Context) {
	if d == nil {
		return
	}
	if d.Registrar != nil {
		unregCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = d.Registrar.Stop(unregCtx)
		cancel()
	}
	if d.MCPServer != nil {
		_ = d.MCPServer.Stop()
	}
	d.Log.Info("iris stopped")
}

// Run brings iris up, writes its PID file, blocks until ctx is canceled,
// then gracefully shuts down.
func Run(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	d, err := Start(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer d.Stop(context.Background())

	if err := os.WriteFile(d.Cfg.PIDPath(), []byte(fmt.Sprintf("%d", os.Getpid())), 0o644); err != nil {
		log.Warn("write pidfile", "path", d.Cfg.PIDPath(), "err", err)
	}
	defer os.Remove(d.Cfg.PIDPath())

	<-ctx.Done()
	return nil
}

// toolDefinitions returns the iris_* tool registrations.
func toolDefinitions() []mcp.ToolDefinition {
	return []mcp.ToolDefinition{
		{
			Name:        "iris_merge_to_master",
			Description: "Merge an argus task's branch into the source repo's default branch (master/main). Resolves the source repo from the argus task_id. Refuses any branch not prefixed `argus/`. Returns the merge SHA and git log.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo and branch from this."},
					"no_ff":   map[string]any{"type": "boolean", "description": "Pass --no-ff to git merge (default true). When false, requires fast-forward."},
					"message": map[string]any{"type": "string", "description": "(optional) Merge commit message (-m <message>)."},
				},
				"required": []string{"task_id"},
			},
		},
	}
}
