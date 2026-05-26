// Package daemon assembles iris's runtime: argus client, MCP server,
// registrar, verbs handlers. The exported Run() is what `iris start
// --foreground` invokes.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
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
	Watcher   *argus.Watcher
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
	mcpSrv.RegisterHandler("iris_push", mcp.NewPushHandler(client))
	mcpSrv.RegisterHandler("iris_gh_pr_create", mcp.NewGHPRCreateHandler(client))
	mcpSrv.RegisterHandler("iris_gh_pr_merge", mcp.NewGHPRMergeHandler(client))
	mcpSrv.RegisterHandler("iris_gh_pr_view", mcp.NewGHPRViewHandler(client))
	mcpSrv.RegisterHandler("iris_gh_pr_ready", mcp.NewGHPRReadyHandler(client))
	mcpSrv.RegisterHandler("iris_gh_pr_comment", mcp.NewGHPRCommentHandler(client))
	mcpSrv.RegisterHandler("iris_gh_pr_close", mcp.NewGHPRCloseHandler(client))
	mcpSrv.RegisterHandler("iris_run_build", mcp.NewRunBuildHandler(client))
	mcpSrv.RegisterHandler("iris_complete_task", mcp.NewCompleteTaskHandler(client))

	registrar := mcp.NewRegistrar(client, mcpSrv.CallbackBaseURL(), auth, log)
	registrar.SetHeartbeat(cfg.MCPHeartbeat)
	for _, def := range toolDefinitions() {
		registrar.Add(def)
	}
	if err := registrar.Start(ctx); err != nil {
		_ = mcpSrv.Stop()
		return nil, fmt.Errorf("iris: register tools: %w", err)
	}

	// Wire recovery: the watcher fires on pid-mtime change or socket-ping
	// failure; the registrar heartbeat fires the same callback as a passive
	// fallback on 404 responses.
	recover := argus.RecoverFunc(ports, client, registrar, log)
	registrar.SetOnHeartbeat404(recover)

	watcher := &argus.Watcher{
		PidPath:   cfg.ArgusPIDPath,
		Ping:      ports.Ping,
		Interval:  argus.DefaultWatcherInterval,
		OnRestart: recover,
		Log:       log,
	}
	watcher.Start(ctx)

	log.Info("iris ready",
		"argus_base_url", argusBaseURL,
		"mcp_addr", mcpSrv.Addr(),
		"state_dir", cfg.StateDir,
	)

	return &Daemon{
		Cfg: cfg, Log: log, Argus: client, Ports: ports,
		MCPServer: mcpSrv, Registrar: registrar, Watcher: watcher,
	}, nil
}

// Stop gracefully shuts every subsystem down. Watcher and Registrar
// teardown run in parallel under a shared 10s budget so worst-case
// shutdown stays well under launchd's 20s KillTimeout. The MCP server
// shuts down last (sequentially) because there's no good way to abort
// in-flight handlers; that's bounded by the server's own 5s deadline.
func (d *Daemon) Stop(ctx context.Context) {
	if d == nil {
		return
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	if d.Watcher != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Watcher.Stop(shutdownCtx)
		}()
	}
	if d.Registrar != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.Registrar.Stop(shutdownCtx)
		}()
	}
	wg.Wait()

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
		{
			Name:        "iris_push",
			Description: "Push the argus task's branch to origin from the canonical source repo. Resolves source repo and branch from task_id. Refuses to push the default branch. Returns the remote SHA.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":          map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo and branch from this."},
					"force_with_lease": map[string]any{"type": "boolean", "description": "Pass --force-with-lease to git push (default false). Safe form of --force; checks the upstream matches."},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "iris_gh_pr_create",
			Description: "Open a GitHub pull request for the argus task's branch using the host's gh CLI. Resolves source repo and branch from task_id. Refuses to open a PR from the default branch. Returns the PR number and URL.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo and branch from this."},
					"title":   map[string]any{"type": "string", "description": "PR title."},
					"body":    map[string]any{"type": "string", "description": "(optional) PR body. When omitted, gh's default applies."},
					"draft":   map[string]any{"type": "boolean", "description": "Open the PR as a draft (default false)."},
				},
				"required": []string{"task_id", "title"},
			},
		},
		{
			Name:        "iris_gh_pr_merge",
			Description: "Merge a GitHub pull request via the host's gh CLI. Caller passes pr_number and strategy (squash|merge|rebase). v1 does not pre-check CI; gh's own merge command surfaces a failure when required checks are red.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":   map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo from this."},
					"pr_number": map[string]any{"type": "integer", "minimum": 1, "description": "GitHub PR number to merge."},
					"strategy":  map[string]any{"type": "string", "enum": []string{"squash", "merge", "rebase"}, "default": "squash", "description": "Merge strategy."},
				},
				"required": []string{"task_id", "pr_number"},
			},
		},
		{
			Name:        "iris_gh_pr_view",
			Description: "Read a GitHub PR's state via the host's gh CLI. Shells out to `gh pr view <pr> --json state,checks,reviews,mergeable,headRefName,baseRefName,isDraft,statusCheckRollup` in the resolved source repo and returns the parsed JSON unchanged. For \"is this PR ready to merge\" agent loops.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":   map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo from this."},
					"pr_number": map[string]any{"type": "integer", "minimum": 1, "description": "GitHub PR number to view."},
				},
				"required": []string{"task_id", "pr_number"},
			},
		},
		{
			Name:        "iris_gh_pr_ready",
			Description: "Take a draft GitHub PR out of draft via the host's gh CLI. Pre-fetches isDraft so the result reports whether the call moved state; idempotent if the PR is already ready.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":   map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo from this."},
					"pr_number": map[string]any{"type": "integer", "minimum": 1, "description": "GitHub PR number to mark ready."},
				},
				"required": []string{"task_id", "pr_number"},
			},
		},
		{
			Name:        "iris_gh_pr_comment",
			Description: "Post a comment to a GitHub PR via the host's gh CLI. Returns the parsed comment URL; if gh's output cannot be parsed, returns a parse_warning with the raw stdout.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":   map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo from this."},
					"pr_number": map[string]any{"type": "integer", "minimum": 1, "description": "GitHub PR number to comment on."},
					"body":      map[string]any{"type": "string", "minLength": 1, "description": "Comment body. Required and must be non-empty."},
				},
				"required": []string{"task_id", "pr_number", "body"},
			},
		},
		{
			Name:        "iris_gh_pr_close",
			Description: "Close a GitHub PR without merging via the host's gh CLI. Optional delete_branch flag passes --delete-branch to gh.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":       map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo from this."},
					"pr_number":     map[string]any{"type": "integer", "minimum": 1, "description": "GitHub PR number to close."},
					"delete_branch": map[string]any{"type": "boolean", "default": false, "description": "Pass --delete-branch to gh; deletes the source branch after closing."},
				},
				"required": []string{"task_id", "pr_number"},
			},
		},
		{
			Name:        "iris_run_build",
			Description: "Run the project's build for an argus task in its worktree. Looks for an executable `script/iris-build` first, then `Makefile` (build target). Returns command, exit code, and combined output. On non-zero exit the response is an error that still carries the captured output so callers see compile errors.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the worktree from this."},
					"target":  map[string]any{"type": "string", "description": "(optional) Build target/argument passed verbatim to the script or as `make build <target>`."},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "iris_complete_task",
			Description: "Composite ship-it sequence: merge task branch into default branch, push default branch to origin, delete remote task branch, mark argus task complete, archive. Each sub-step is a checkpoint; partial failures return the checkpoints reached so a retry can resume.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":        map[string]any{"type": "string", "description": "Argus task ID."},
					"merge_strategy": map[string]any{"type": "string", "enum": []string{"no_ff", "ff_only"}, "default": "no_ff", "description": "Merge strategy used by the embedded merge_to_master step."},
				},
				"required": []string{"task_id"},
			},
		},
	}
}
