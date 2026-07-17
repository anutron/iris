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
	mcpSrv.RegisterHandler("iris_run_checks", mcp.NewRunChecksHandler(client))
	mcpSrv.RegisterHandler("iris_set_dogfood", mcp.NewSetDogfoodHandler(client))
	mcpSrv.RegisterHandler("iris_set_local_config", mcp.NewSetLocalConfigHandler(client))
	mcpSrv.RegisterHandler("iris_ship_feature", mcp.NewShipFeatureHandler(client))
	mcpSrv.RegisterHandler("iris_complete_task", mcp.NewCompleteTaskHandler(client))
	mcpSrv.RegisterHandler("iris_fetch", mcp.NewFetchHandler(client))
	mcpSrv.RegisterHandler("iris_branch_delete_remote", mcp.NewBranchDeleteRemoteHandler(client))
	mcpSrv.RegisterHandler("iris_branch_create", mcp.NewBranchCreateHandler(client))
	mcpSrv.RegisterHandler("iris_cherry_pick", mcp.NewCherryPickHandler(client))
	mcpSrv.RegisterHandler("iris_checkout", mcp.NewCheckoutHandler(client))
	mcpSrv.RegisterHandler("iris_tag", mcp.NewTagHandler(client))
	mcpSrv.RegisterHandler("iris_reload", mcp.NewReloadHandler(client))
	mcpSrv.RegisterHandler("iris_publish", mcp.NewPublishHandler(client))
	mcpSrv.RegisterHandler("iris_validate_config", mcp.NewValidateConfigHandler(client))
	mcpSrv.RegisterHandler("iris_ls", mcp.NewLsHandler())
	mcpSrv.RegisterHandler("iris_status", mcp.NewStatusHandler(client))

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
			Description: "Merge an argus task's branch into the source repo's default branch (master/main). Resolves the source repo from the argus task_id. Refuses any branch not prefixed `argus/`. Does NOT delete the task branch or the worktree — call `iris_branch_delete_remote` and let argus archive the worktree (or use `iris_complete_task` for the full ship-it sequence). The result reports factual postconditions: `task_branch_still_exists` and `worktree_still_present` are always `true`. When `.iris.toml` declares a `[post_merge]` hook, iris runs it after a successful real merge and captures the outcome in `post_merge` (exit_code/stdout/stderr/duration_ms/error). With `dry_run: true`, iris previews the merge via `git merge --no-commit --no-ff` and `merge --abort`, returning `would_succeed`, `files_changed`, and `conflicts` without committing or running the hook.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo and branch from this."},
					"no_ff":   map[string]any{"type": "boolean", "description": "Pass --no-ff to git merge (default true). When false, requires fast-forward."},
					"message": map[string]any{"type": "string", "description": "(optional) Merge commit message (-m <message>)."},
					"dry_run": map[string]any{"type": "boolean", "description": "Preview the merge: run `git merge --no-commit --no-ff <branch>`, capture files_changed + conflicts, then `merge --abort`. No commit, no post_merge hook. Defaults to false."},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "iris_push",
			Description: "Push the argus task's branch to a remote from the canonical source repo. Resolves source repo and branch from task_id. Pushes to `origin` by default; pass `remote` to push to a different CONFIGURED remote (a name, never a URL) — e.g. to push a branch to an upstream you have write access to so its CI runs (cross-fork PRs from a fork don't run CI). Refuses to push the default branch. Returns the effective remote and remote SHA.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":          map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo and branch from this."},
					"force_with_lease": map[string]any{"type": "boolean", "description": "Pass --force-with-lease to git push (default false). Safe form of --force; checks the upstream matches."},
					"branch":           map[string]any{"type": "string", "description": "(optional) Branch to push instead of the task's resolved branch. MUST NOT be the default branch."},
					"remote":           map[string]any{"type": "string", "description": "(optional) Push to this remote instead of `origin`. MUST be a remote already configured in the source repo (a name like `upstream`, not a URL); iris validates it exists and never adds remotes."},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "iris_gh_pr_create",
			Description: "Open a GitHub pull request for the argus task's branch using the host's gh CLI. Resolves source repo and branch from task_id. Target selection: if `base_repo` is given, opens a SAME-REPO PR on that owner/repo (use this to PR into an upstream you pushed a branch to via `iris_push remote=...`, so CI runs); else if origin is a fork, opens a CROSS-FORK PR into the upstream parent (note: GitHub does not run CI on cross-fork PRs from a fork); else opens a same-repo PR on origin. Refuses to open a PR from the default branch. Returns the PR number and URL.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":   map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo and branch from this."},
					"title":     map[string]any{"type": "string", "description": "PR title."},
					"body":      map[string]any{"type": "string", "description": "(optional) PR body. When omitted, gh's default applies."},
					"draft":     map[string]any{"type": "boolean", "description": "Open the PR as a draft (default false)."},
					"head":      map[string]any{"type": "string", "description": "(optional) Head branch to open the PR for instead of the task's resolved branch. MUST NOT be the default branch."},
					"base_repo": map[string]any{"type": "string", "description": "(optional) Open a same-repo PR on this owner/repo (e.g. `drn/argus`) and skip fork auto-detection. The head branch must already exist there (push it first with `iris_push remote=...`). Takes precedence over fork detection; the head is not fork-qualified."},
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
			Description: "Read a GitHub PR's state via the host's gh CLI. Shells out to `gh pr view <pr> --json state,reviews,mergeable,headRefName,baseRefName,isDraft,statusCheckRollup` in the resolved source repo and returns the parsed JSON unchanged. CI check state is in statusCheckRollup. For \"is this PR ready to merge\" agent loops.",
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
			Name:        "iris_run_checks",
			Description: "Run a repo-defined quality check for an argus task in its worktree, host-side. Runs the executable `script/iris-check <check>` (e.g. check=\"lint\", \"test\", \"security\") and returns command, exit code, and combined output. Script-only — there is no Makefile fallback; if `script/iris-check` is absent or non-executable the call errors naming the expected path. On non-zero exit the response is an error that still carries the full check output (the real rubocop/rspec/brakeman text), so sandboxed agents see check failures verbatim without reading CI logs. `check` is a single token passed as an argv element to the repo-controlled script, not a shell string.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the worktree from this."},
					"check":   map[string]any{"type": "string", "description": "Check to run, passed as the first positional argument to `script/iris-check` (e.g. \"lint\", \"test\", \"security\"). Required and non-empty."},
				},
				"required": []string{"task_id", "check"},
			},
		},
		{
			Name:        "iris_set_dogfood",
			Description: "Atomically point the configured dogfood branch at a worker-composed commit SHA, persist a structured manifest of what that SHA contains, and rebuild/restart the service via the existing reload machinery. Iris does NOT compose — the agent builds the SHA (cherry-pick/merge/rebase, conflict resolution) and hands iris the finished commit. Refuses any repo whose .iris.toml does not declare `dogfood_branch`. Refuses a sha that is NOT a descendant of the current dogfood SHA (it would drop commits) unless force=true, which proceeds with a warning. The ref move is worktree-guarded: `git branch -f` normally, but `git reset --hard` in the worktree when the dogfood branch is checked out. Writes the manifest before moving the ref (durable-first); a reset failure leaves the manifest ahead, surfaced as drift by iris_status. Returns previous_sha (\"\" when the branch was newly created), new_sha, and the reload result.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "(optional) Argus task ID. Iris resolves the source repo from this; omit for self-target (iris-on-iris)."},
					"sha":     map[string]any{"type": "string", "description": "Full commit SHA to point the dogfood branch at. MUST be reachable in the source repo's object database."},
					"force":   map[string]any{"type": "boolean", "description": "(optional, default false) Override the commit-dropping ancestry guard. When the sha is NOT a descendant of the current dogfood SHA, set_dogfood refuses (deploying it would drop commits) unless force is true, in which case it proceeds and emits a prominent warning. Iris never recomposes on your behalf — recompose onto the current dogfood SHA or pass force to intentionally drop."},
					"manifest": map[string]any{
						"type":        "object",
						"description": "Structured record of what composes the SHA. Descriptive only — iris does not validate that layered SHAs are reachable from `sha`.",
						"properties": map[string]any{
							"base": map[string]any{
								"type":        "object",
								"description": "The upstream base the SHA was composed on top of.",
								"properties": map[string]any{
									"ref": map[string]any{"type": "string", "description": "Base ref name, e.g. \"main\"."},
									"sha": map[string]any{"type": "string", "description": "Base SHA at compose time."},
								},
								"required": []string{"ref", "sha"},
							},
							"layered": map[string]any{
								"type":        "array",
								"description": "Ordered list of branches composed in.",
								"items": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"name":    map[string]any{"type": "string", "description": "Feature/branch name."},
										"sha":     map[string]any{"type": "string", "description": "The branch SHA composed in."},
										"applied": map[string]any{"type": "string", "description": "(optional) How it was applied, e.g. \"cherry-pick\", \"merge\". Descriptive only."},
									},
									"required": []string{"name", "sha"},
								},
							},
							"note": map[string]any{"type": "string", "description": "(optional) Free-text from the agent."},
						},
						"required": []string{"base"},
					},
				},
				"required": []string{"sha", "manifest"},
			},
		},
		{
			Name:        "iris_ship_feature",
			Description: "Ship a feature branch to origin's default branch via a GitHub pull request. via=\"pr\" pushes the branch and opens a PR targeting the default branch, then stops — the worker returns after review. It never merges, fetches, or touches the dogfood branch/manifest. Refuses the default branch, a branch that does not exist locally, and any via other than \"pr\" (pr-auto is a later stage). pr_title defaults to the branch's last commit subject when omitted. Returns shipped, pr_number, pr_url, and (for pr mode) merged=false / fetched=false.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":      map[string]any{"type": "string", "description": "(optional) Argus task ID. Iris resolves the source repo from this; omit for self-target."},
					"branch":       map[string]any{"type": "string", "description": "Local feature branch to ship. MUST exist locally and MUST NOT be the default branch."},
					"via":          map[string]any{"type": "string", "enum": []string{"pr"}, "description": "Ship mode. Only \"pr\" is supported in this stage (push + open PR, then stop)."},
					"pr_title":     map[string]any{"type": "string", "description": "(optional) PR title. Defaults to the branch's last commit subject."},
					"pr_body":      map[string]any{"type": "string", "description": "(optional) PR body."},
					"merge_method": map[string]any{"type": "string", "enum": []string{"squash", "merge", "rebase"}, "description": "(optional) Merge method; defaults to \"squash\". Unused in pr mode."},
				},
				"required": []string{"branch", "via"},
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
		{
			Name:        "iris_fetch",
			Description: "Run `git fetch origin` in the argus task's source repo under the per-source-repo lock. Returns the list of refs whose tracking SHAs changed.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo from this."},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "iris_branch_delete_remote",
			Description: "Delete a remote branch on origin via `git push origin :<branch>`. Refuses the default branch and any branch absent from origin. Returns the branch's prior remote SHA.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo from this."},
					"branch":  map[string]any{"type": "string", "description": "Remote branch name to delete (required, non-empty, MUST NOT be the default branch)."},
				},
				"required": []string{"task_id", "branch"},
			},
		},
		{
			Name:        "iris_branch_create",
			Description: "Create a branch in the resolved source repo from an arbitrary ref via `git branch <name> <base_ref>`. Does NOT change the source repo's current checkout. Refuses the default branch name, leading-dash refnames, invalid git refs (per `git check-ref-format --branch`), and pre-existing local branches. Pair with `iris:checkout` to switch.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":  map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo from this."},
					"name":     map[string]any{"type": "string", "description": "New branch name. MUST NOT begin with '-', equal the default branch (or main/master), or already exist locally."},
					"base_ref": map[string]any{"type": "string", "description": "Ref to branch from — e.g. `origin/master`, a SHA, a tag. MUST NOT begin with '-'. Iris does not pre-fetch; call `iris_fetch` first if you need the latest origin state."},
				},
				"required": []string{"task_id", "name", "base_ref"},
			},
		},
		{
			Name:        "iris_cherry_pick",
			Description: "Cherry-pick a commit onto a target branch in the source repo. Checks out `target_branch` then runs `git cherry-pick <commit>` under the per-source-repo lock; the pair is atomic from other iris callers' view. On conflict, runs `git cherry-pick --abort` and returns a structured error carrying the conflict paths; the source repo lands back on `target_branch` with a clean working tree. Refuses cherry-picking onto the default branch (use `iris_merge_to_master` for that).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":       map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo from this."},
					"commit":        map[string]any{"type": "string", "description": "Commit-ish to cherry-pick (SHA, branch name, etc.). MUST NOT begin with '-'."},
					"target_branch": map[string]any{"type": "string", "description": "Local branch to apply the commit onto. MUST NOT begin with '-', equal the default branch, or be absent locally."},
				},
				"required": []string{"task_id", "commit", "target_branch"},
			},
		},
		{
			Name:        "iris_checkout",
			Description: "Switch the source repo to a branch. With `force=false` (default), runs plain `git checkout <branch>` and propagates git's refusal for dirty working trees or in-progress merges/cherry-picks. With `force=true`, runs best-effort `git merge --abort`, `git cherry-pick --abort`, `git rebase --abort`, then `git checkout -f <branch>` — the explicit recovery path for a source repo stuck mid-operation. `prior_branch` and `prior_head` reflect the state before any recovery so the discarded SHA is recoverable via reflog.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo from this."},
					"branch":  map[string]any{"type": "string", "description": "Branch to switch to. MUST NOT begin with '-'."},
					"force":   map[string]any{"type": "boolean", "default": false, "description": "Abort any in-progress merge/cherry-pick/rebase and discard uncommitted changes before switching. Destructive — pair with reflog for recovery."},
				},
				"required": []string{"task_id", "branch"},
			},
		},
		{
			Name:        "iris_tag",
			Description: "Create an annotated git tag at the SHA of origin/<default-branch> and push it to origin. Refuses if the tag already exists locally or on origin. Returns the tag SHA.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo from this."},
					"tag":     map[string]any{"type": "string", "description": "Tag name to create (required, non-empty)."},
					"message": map[string]any{"type": "string", "description": "(optional) Annotation message. Defaults to \"Released by iris\" when empty."},
				},
				"required": []string{"task_id", "tag"},
			},
		},
		{
			Name:        "iris_reload",
			Description: "Live-upgrade an iris-managed daemon. Pulls the default branch, runs the project-declared build, then dispatches to the project-declared restart mechanism in `.iris.toml`. Self-vs-cross detection is automatic. task_id and path are mutually exclusive; both omitted targets iris itself.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":         map[string]any{"type": "string", "description": "(optional) Argus task ID. Iris resolves the source repo from this. Omit for self-target."},
					"path":            map[string]any{"type": "string", "description": "(optional) Absolute filesystem path to a source repo. Omit for self-target."},
					"no_pull":         map[string]any{"type": "boolean", "description": "Skip the git fetch + ff-merge and build from current HEAD (default false)."},
					"timeout_seconds": map[string]any{"type": "integer", "description": "(optional) Per-step timeout override in seconds."},
				},
			},
		},
		{
			Name:        "iris_publish",
			Description: "From an argus worktree, update the source repo's currently-checked-out branch to the worktree's HEAD, then rebuild and restart via the project's .iris.toml. Default is ff-only; pass reset=true for hard reset (atomic ref+working-tree). Optional push=true also pushes the target branch to origin (subject to the same default-branch refusal as iris:push). v1.2 constraint: the target branch must equal the source repo's current branch.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Argus task ID. Iris resolves the source repo and worktree from this."},
					"branch":  map[string]any{"type": "string", "description": "(optional) Target branch in the source repo. Defaults to the source repo's currently-checked-out branch; must equal it (v1.2)."},
					"push":    map[string]any{"type": "boolean", "description": "Push the target branch to origin after the local update (default false). Subject to default-branch refusal."},
					"reset":   map[string]any{"type": "boolean", "description": "Hard-reset the source repo to the worktree's HEAD (atomic ref+working tree). Default false (ff-only)."},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "iris_validate_config",
			Description: "Parse and cross-validate a `.iris.toml` file at the resolved source repo. No side effects (no pull, build, restart, or audit write). Returns valid/invalid plus structured errors with line numbers and remediation hints.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "(optional) Argus task ID. Omit for self-target."},
					"path":    map[string]any{"type": "string", "description": "(optional) Absolute filesystem path. Omit for self-target."},
				},
			},
		},
		{
			Name:        "iris_set_local_config",
			Description: "Write (or merge into) `.iris.local.toml` at the resolved source repo's root with worker-supplied per-developer fields (dogfood_branch, ship_ci_timeout_seconds). Refuses any field whose taxonomy classification is not `local` (shared fields belong in `.iris.toml`). Validates per-field rules (git ref syntax, dogfood_branch != default_branch, non-negative timeout). Acquires the source-repo lock for the atomic read-modify-write (tmp + rename). Idempotent: re-setting an existing value is a no-op write that still returns written=true. Does NOT trigger reload — config takes effect on the next reload/status.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "(optional) Argus task ID. Iris resolves the source repo from this; omit for self-target."},
					"fields": map[string]any{
						"type":                 "object",
						"description":          "Map of local-tagged field name to value. Valid names: dogfood_branch (string, valid git ref, != default_branch), ship_ci_timeout_seconds (non-negative integer).",
						"additionalProperties": true,
					},
					"delete": map[string]any{
						"type":        "array",
						"description": "Local-tagged field names to remove from the file. Names not present are silently ignored. Refused if any name is shared-tagged or unknown.",
						"items":       map[string]any{"type": "string"},
					},
				},
			},
		},
		{
			Name:        "iris_ls",
			Description: "List managed systems iris has reloaded recently. Reads `~/.iris/reload-history.jsonl` and projects per-system aggregates (last_reload_at, last_outcome, total_reload_count, total_failure_count). No registry; the audit log is the source of truth.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer", "description": "(optional) Maximum number of entries (default 50)."},
					"since": map[string]any{"type": "string", "description": "(optional) RFC3339 timestamp; exclude entries before this time."},
				},
			},
		},
		{
			Name:        "iris_status",
			Description: "For one managed system, report the parsed `.iris.toml`, current git state (HEAD, branch, default branch, origin SHA, working-tree-clean), the matching argus task when iris can find one whose worktree_path equals the resolved source repo (`argus_task`, null otherwise), and the most recent reload outcome from the audit log. A missing `.iris.toml` is silent (`config: null`, no warning); parse errors still surface as warnings. No side effects. task_id and path are mutually exclusive; both omitted targets iris itself.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "(optional) Argus task ID. Omit for self-target."},
					"path":    map[string]any{"type": "string", "description": "(optional) Absolute filesystem path. Omit for self-target."},
				},
			},
		},
	}
}
