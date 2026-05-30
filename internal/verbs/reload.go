// Package verbs: reload implements iris:reload — live-upgrade an
// iris-managed daemon by pulling the default branch, running the project's
// build, and dispatching to a project-declared restart mechanism.
//
// See openspec/changes/add-daemon-self-management/design.md.

package verbs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// exitFunc is the indirection used for self-reload's os.Exit. Tests
// override it to capture the requested exit code without actually
// terminating the test process.
var exitFunc = os.Exit

// ErrCLISelfReloadUnsupported is returned when `iris reload` is invoked from
// the CLI against the iris source repo itself. The exit_code restart
// mechanism only respawns the process that exited, and the CLI is
// short-lived, so the restart cannot land. The message body redirects the
// caller to the three working alternatives. The grep-friendly token
// `cli-self-reload-not-supported` appears here and in the audit log.
var ErrCLISelfReloadUnsupported = errors.New(
	"cli-self-reload-not-supported: self-reload from CLI is not supported: the exit_code restart mechanism\n" +
		"only respawns the process that exited, and the CLI is short-lived. Use\n" +
		"one of:\n" +
		"  - invoke iris_reload via MCP from a Claude session (primary path)\n" +
		"  - iris reload <other-iris-managed-project>  for cross-target\n" +
		"  - iris run-build && launchctl kickstart -k gui/$UID/<label>\n" +
		"    to manually bounce the daemon after a build")

// ReloadInput is the public input shape for iris:reload.
type ReloadInput struct {
	TaskID         string
	Path           string
	NoPull         bool
	TimeoutSeconds int
	// Caller identifies who is invoking the reload — argus task_id when
	// the call came in via MCP, "cli" for direct CLI invocation, or "self"
	// otherwise.
	Caller string
}

// ReloadResult is the structured result. The same shape appears in the
// audit log (with the timestamp and caller fields added).
type ReloadResult struct {
	TargetSourceRepo string   `json:"target_source_repo"`
	Mode             string   `json:"mode"`
	Pulled           bool     `json:"pulled"`
	PrePullSha       string   `json:"pre_pull_sha"`
	PostPullSha      string   `json:"post_pull_sha"`
	PreFlightOutput  string   `json:"pre_flight_output"`
	BuildOutput      string   `json:"build_output"`
	RestartMechanism string   `json:"restart_mechanism"`
	RestartOutput    string   `json:"restart_output"`
	VerifyOutput     string   `json:"verify_output"`
	RestartPending   bool     `json:"restart_pending"`
	Warnings         []string `json:"warnings"`
}

// Reload performs the full reload sequence.
//
// The function blocks until restart returns (cross-reload) or until the
// response is ready to flush (self-reload). For self-reload, after the
// caller has consumed the response (via the returned RestartCallback),
// the deferred exit fires from a goroutine to release the lock and exit
// the process so the LaunchAgent respawns from the new binary.
func Reload(ctx context.Context, client *argus.Client, in ReloadInput) (*ReloadResult, error) {
	caller := in.Caller
	if caller == "" {
		caller = "unknown"
	}

	// 1. Resolve target (no side effects, no lock held)
	target, err := ResolveTarget(ctx, client, in.TaskID, in.Path)
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, Outcome: "failure",
			FailureReason: err.Error(),
		})
		return nil, err
	}

	// 2. Self-vs-cross detection
	isSelf, err := isSelfTarget(ctx, target.SourceRepo)
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo,
			Outcome: "failure", FailureReason: err.Error(),
		})
		return nil, err
	}
	mode := "cross"
	if isSelf {
		mode = "self"
	}

	// 2a. Refuse CLI self-reload before any side effects (no toml load, no
	// working-tree check, no lock, no pull, no build). The exit_code restart
	// only respawns the exiting process; the CLI is short-lived, so the
	// restart cannot land.
	if caller == "cli" && isSelf {
		writeAudit(AuditEntry{
			Caller:           caller,
			TargetSourceRepo: target.SourceRepo,
			Mode:             "self",
			Outcome:          "failure",
			FailureReason:    ErrCLISelfReloadUnsupported.Error(),
		})
		return nil, ErrCLISelfReloadUnsupported
	}

	// 3. Pre-pull tree-state refusals.
	//
	// IMPORTANT: `.iris.toml` content refusals (missing file, malformed TOML,
	// schema/mechanism validation) do NOT run here. They run AFTER the pull,
	// against the post-pull config the rebuilt-and-restarted binary will
	// actually consume — see step 6. Validating the pre-pull file would judge
	// stale state and, fatally, would reject an additive field that the very
	// pull is about to deliver (the old binary's decoder treats it as unknown).
	tomlPath := filepath.Join(target.SourceRepo, config.IrisTomlFilename)

	// Working tree clean?
	if err := checkCleanTree(ctx, target.SourceRepo); err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
			Outcome: "failure", FailureReason: err.Error(),
		})
		return nil, err
	}

	// Resolve default branch for the fetch target. The override comes from a
	// LENIENT pre-pull peek of `.iris.toml` (override only, never refuses); on
	// a missing/malformed/forward-compatible file the peek yields "" and the
	// git origin/HEAD → "main" fallback applies.
	warnings := []string{}
	defaultBranch, dbWarn, err := resolveDefaultBranch(ctx, target.SourceRepo, config.PeekDefaultBranch(tomlPath))
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
			Outcome: "failure", FailureReason: err.Error(),
		})
		return nil, err
	}
	if dbWarn != "" {
		warnings = append(warnings, dbWarn)
	}

	// On branch?
	currentBranch, err := currentBranch(ctx, target.SourceRepo)
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
			Outcome: "failure", FailureReason: err.Error(),
		})
		return nil, err
	}
	if currentBranch != defaultBranch {
		err := fmt.Errorf("source repo %s is on branch %q; expected default branch %q", target.SourceRepo, currentBranch, defaultBranch)
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
			Outcome: "failure", FailureReason: err.Error(),
		})
		return nil, err
	}

	// Origin reachable?
	if _, err := runGit(ctx, target.SourceRepo, "remote", "get-url", "origin"); err != nil {
		err = fmt.Errorf("origin remote missing or unreachable: %w", err)
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
			Outcome: "failure", FailureReason: err.Error(),
		})
		return nil, err
	}

	// 4. Acquire lock — every subsequent step must release it before returning.
	mu := lockSourceRepo(target.SourceRepo)
	lockHeld := true
	releaseLock := func() {
		if lockHeld {
			mu.Unlock()
			lockHeld = false
		}
	}
	defer releaseLock()

	// 5. Pull (fetch + ff-only merge) unless no_pull
	prePullSha, err := runGit(ctx, target.SourceRepo, "rev-parse", "HEAD")
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
			Outcome: "failure", FailureReason: err.Error(),
		})
		return nil, err
	}
	prePullSha = strings.TrimSpace(prePullSha)
	postPullSha := prePullSha
	pulled := false
	if !in.NoPull {
		if _, err := runGit(ctx, target.SourceRepo, "fetch", "origin", defaultBranch); err != nil {
			writeAudit(AuditEntry{
				Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
				PrePullSha: prePullSha, PostPullSha: prePullSha,
				Outcome: "failure", FailureReason: err.Error(),
			})
			return nil, fmt.Errorf("git fetch origin %s: %w", defaultBranch, err)
		}
		// remote SHA for the diverge-check error message
		remoteRef := "origin/" + defaultBranch
		if _, err := runGit(ctx, target.SourceRepo, "merge", "--ff-only", remoteRef); err != nil {
			remoteSha, _ := runGit(ctx, target.SourceRepo, "rev-parse", remoteRef)
			reason := fmt.Sprintf("fast-forward not possible: local=%s remote=%s",
				prePullSha, strings.TrimSpace(remoteSha))
			writeAudit(AuditEntry{
				Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
				PrePullSha: prePullSha, PostPullSha: prePullSha,
				Outcome: "failure", FailureReason: reason,
			})
			return nil, fmt.Errorf("%s", reason)
		}
		sha, err := runGit(ctx, target.SourceRepo, "rev-parse", "HEAD")
		if err != nil {
			writeAudit(AuditEntry{
				Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
				Outcome: "failure", FailureReason: err.Error(),
			})
			return nil, err
		}
		postPullSha = strings.TrimSpace(sha)
		pulled = true
	}

	// 6. Load + validate the POST-pull `.iris.toml` — the config the rebuilt
	// binary will consume. Forward-compatible mode: an additive field freshly
	// arrived from origin is tolerated as a warning (the old decoder would
	// otherwise reject it as unknown); schema_version mismatch and malformed
	// TOML remain hard refusals. This is where the missing-file and validation
	// refusals fire — after the pull, not before it.
	doc, verrs, tomlWarnings, err := config.LoadIrisTomlMode(tomlPath, isSelf, config.LoadMode{TolerateUnknownFields: true})
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
			Pulled: pulled, PrePullSha: prePullSha, PostPullSha: postPullSha,
			Outcome: "failure", FailureReason: err.Error(),
		})
		return nil, err
	}
	if doc == nil {
		reason := fmt.Sprintf("%s not found at %s", config.IrisTomlFilename, tomlPath)
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
			Pulled: pulled, PrePullSha: prePullSha, PostPullSha: postPullSha,
			Outcome: "failure", FailureReason: reason,
		})
		return nil, fmt.Errorf("%s", reason)
	}
	if len(verrs) > 0 {
		reason := joinValidationErrors(verrs)
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
			Pulled: pulled, PrePullSha: prePullSha, PostPullSha: postPullSha,
			Outcome: "failure", FailureReason: reason,
		})
		return nil, fmt.Errorf("%s invalid: %s", config.IrisTomlFilename, reason)
	}
	warnings = append(warnings, tomlWarnings...)

	// 7. [pre_flight] hook — runs against the freshly-pulled tree (reading the
	// hook definition from the post-pull config), after validation and before
	// the build. On failure nothing is built or restarted.
	var preFlightOutput string
	if doc.PreFlight != nil {
		out, err := runHook(ctx, target.SourceRepo, *doc.PreFlight, config.DefaultHookTimeoutSeconds)
		preFlightOutput = out
		if err != nil {
			err = fmt.Errorf("pre_flight hook failed: %w; output:\n%s", err, out)
			writeAudit(AuditEntry{
				Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
				Pulled: pulled, PrePullSha: prePullSha, PostPullSha: postPullSha,
				PreFlightOutput: preFlightOutput,
				Outcome:         "failure", FailureReason: err.Error(),
				Warnings:        warnings,
			})
			return nil, err
		}
	}

	// 8. Build
	buildOutput, err := runBuildBlock(ctx, target.SourceRepo, doc.Build)
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
			Pulled: pulled, PrePullSha: prePullSha, PostPullSha: postPullSha,
			PreFlightOutput: preFlightOutput, BuildOutput: buildOutput,
			Outcome: "failure", FailureReason: err.Error(),
		})
		return nil, err
	}

	// 8. Restart dispatch
	restartOutput, restartWarn, err := dispatchRestart(ctx, doc.Restart, isSelf)
	if restartWarn != "" {
		warnings = append(warnings, restartWarn)
	}
	if err != nil {
		writeAudit(AuditEntry{
			Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
			Pulled: pulled, PrePullSha: prePullSha, PostPullSha: postPullSha,
			PreFlightOutput: preFlightOutput, BuildOutput: buildOutput,
			RestartMechanism: string(doc.Restart.Mechanism), RestartOutput: restartOutput,
			Outcome: "failure", FailureReason: err.Error(),
			Warnings: warnings,
		})
		return nil, err
	}

	// 9. [verify] (cross only)
	var verifyOutput string
	if !isSelf && doc.Verify != nil {
		out, vErr := runHook(ctx, target.SourceRepo, *doc.Verify, config.DefaultVerifyTimeoutSeconds)
		verifyOutput = out
		if vErr != nil {
			err = fmt.Errorf("verify hook failed: %w; output:\n%s", vErr, out)
			writeAudit(AuditEntry{
				Caller: caller, TargetSourceRepo: target.SourceRepo, Mode: mode,
				Pulled: pulled, PrePullSha: prePullSha, PostPullSha: postPullSha,
				PreFlightOutput: preFlightOutput, BuildOutput: buildOutput,
				RestartMechanism: string(doc.Restart.Mechanism), RestartOutput: restartOutput,
				VerifyOutput: verifyOutput,
				Outcome:      "failure", FailureReason: err.Error(),
				Warnings: warnings,
			})
			return nil, err
		}
	}

	result := &ReloadResult{
		TargetSourceRepo: target.SourceRepo,
		Mode:             mode,
		Pulled:           pulled,
		PrePullSha:       prePullSha,
		PostPullSha:      postPullSha,
		PreFlightOutput:  preFlightOutput,
		BuildOutput:      buildOutput,
		RestartMechanism: string(doc.Restart.Mechanism),
		RestartOutput:    restartOutput,
		VerifyOutput:     verifyOutput,
		RestartPending:   isSelf,
		Warnings:         warnings,
	}

	writeAudit(AuditEntry{
		Caller:           caller,
		TargetSourceRepo: target.SourceRepo,
		Mode:             mode,
		Pulled:           pulled,
		PrePullSha:       prePullSha,
		PostPullSha:      postPullSha,
		PreFlightOutput:  preFlightOutput,
		BuildOutput:      buildOutput,
		RestartMechanism: string(doc.Restart.Mechanism),
		RestartOutput:    restartOutput,
		VerifyOutput:     verifyOutput,
		RestartPending:   isSelf,
		Warnings:         warnings,
		Outcome:          "success",
	})

	// 10. Self-reload: schedule exit AFTER response is flushed.
	if isSelf && doc.Restart.Mechanism == config.MechanismExitCode {
		code := doc.Restart.ResolvedExitCode()
		releaseLock()
		scheduleSelfExit(code)
	}
	return result, nil
}

// isSelfTarget compares the resolved target source repo to iris's own.
func isSelfTarget(ctx context.Context, target string) (bool, error) {
	self, err := ResolveSelf(ctx)
	if err != nil {
		// We can't tell whether this is self-reload, but we're allowed to
		// resolve a foreign target. Treat as cross.
		return false, nil
	}
	return EqualSourceRepos(target, self.SourceRepo), nil
}

func resolveDefaultBranch(ctx context.Context, sourceRepo, override string) (string, string, error) {
	if override != "" {
		return override, "", nil
	}
	branch, err := DefaultBranch(ctx, sourceRepo)
	if err == nil {
		return branch, "", nil
	}
	// Fall back to "main" with a warning.
	return "main", fmt.Sprintf("origin/HEAD unset, defaulted to main; run `git -C %s remote set-head origin --auto` to fix", sourceRepo), nil
}

func checkCleanTree(ctx context.Context, sourceRepo string) error {
	out, err := runGit(ctx, sourceRepo, "status", "--porcelain=v1")
	if err != nil {
		return fmt.Errorf("working tree status: %w", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("working tree is dirty:\n%s", strings.TrimSpace(out))
	}
	return nil
}

func runHook(ctx context.Context, sourceRepo string, hook config.HookBlock, defaultSec int) (string, error) {
	if len(hook.Command) == 0 {
		return "", nil
	}
	timeout := time.Duration(hook.ResolvedTimeoutSeconds(defaultSec)) * time.Second
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(hookCtx, hook.Command[0], hook.Command[1:]...)
	cmd.Dir = sourceRepo
	if hook.WorkingDirectory != "" {
		cmd.Dir = filepath.Join(sourceRepo, hook.WorkingDirectory)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start hook %v: %w", hook.Command, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return buf.String(), fmt.Errorf("hook %v exited %w", hook.Command, err)
		}
		return buf.String(), nil
	case <-hookCtx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return buf.String(), fmt.Errorf("hook %v timed out after %s", hook.Command, timeout)
	}
}

func runBuildBlock(ctx context.Context, sourceRepo string, b config.BuildBlock) (string, error) {
	timeout := time.Duration(b.ResolvedTimeoutSeconds()) * time.Second
	buildCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(buildCtx, b.Command[0], b.Command[1:]...)
	cmd.Dir = filepath.Join(sourceRepo, b.ResolvedWorkingDirectory())
	cmd.Env = mergedEnv(b.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start build %v: %w", b.Command, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return buf.String(), fmt.Errorf("build %v failed: %w; output:\n%s", b.Command, err, buf.String())
		}
		return buf.String(), nil
	case <-buildCtx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return buf.String(), fmt.Errorf("build %v timed out after %s", b.Command, timeout)
	}
}

// mergedEnv returns os.Environ() overlaid with the project's extra env.
// Project values win on conflict; iris does not interpolate anything.
func mergedEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
	base := os.Environ()
	out := make([]string, 0, len(base)+len(extra))
	override := make(map[string]bool, len(extra))
	for k := range extra {
		override[k] = true
	}
	for _, kv := range base {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		if !override[kv[:eq]] {
			out = append(out, kv)
		}
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}

// dispatchRestart runs the restart mechanism. Returns (output, warning, err).
// For mechanism="exit_code" returns ("", "", nil) — the caller schedules
// the exit AFTER the response is flushed.
func dispatchRestart(ctx context.Context, r config.RestartBlock, isSelf bool) (string, string, error) {
	switch r.Mechanism {
	case config.MechanismExitCode:
		if !isSelf {
			return "", "", fmt.Errorf("restart mechanism exit_code is self-only")
		}
		return "", "", nil
	case config.MechanismNone:
		return "", "", nil
	case config.MechanismLaunchAgent:
		uid := os.Getuid()
		out, err := runArgv(ctx, 30*time.Second,
			"launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", uid, r.Label))
		if err != nil {
			return out, "", fmt.Errorf("launchctl kickstart: %w; output:\n%s", err, out)
		}
		return out, "", nil
	case config.MechanismLaunchDaemon:
		warn := ""
		if os.Geteuid() != 0 {
			warn = "launchdaemon mechanism selected but iris is not root; the kickstart may fail"
		}
		out, err := runArgv(ctx, 30*time.Second,
			"launchctl", "kickstart", "-k", fmt.Sprintf("system/%s", r.Label))
		if err != nil {
			return out, warn, fmt.Errorf("launchctl kickstart: %w; output:\n%s", err, out)
		}
		return out, warn, nil
	case config.MechanismSignal:
		return dispatchSignal(r)
	case config.MechanismExec:
		timeout := time.Duration(r.ResolvedExecTimeoutSeconds()) * time.Second
		out, err := runArgv(ctx, timeout, r.Command[0], r.Command[1:]...)
		if err != nil {
			return out, "", fmt.Errorf("exec %v: %w; output:\n%s", r.Command, err, out)
		}
		return out, "", nil
	default:
		return "", "", fmt.Errorf("unsupported restart mechanism %q (should have been caught at parse)", r.Mechanism)
	}
}

func dispatchSignal(r config.RestartBlock) (string, string, error) {
	sig, ok := config.SignalByName(r.Signal)
	if !ok {
		return "", "", fmt.Errorf("unknown signal %q", r.Signal)
	}
	pidBytes, err := os.ReadFile(r.PidFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", fmt.Errorf("pid_file %s does not exist", r.PidFile)
		}
		return "", "", fmt.Errorf("read pid_file %s: %w", r.PidFile, err)
	}
	pidStr := strings.TrimSpace(string(pidBytes))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return "", "", fmt.Errorf("pid_file %s does not contain a valid PID (got %q)", r.PidFile, pidStr)
	}
	if err := syscall.Kill(pid, sig); err != nil {
		return fmt.Sprintf("kill(%d, %s): %v", pid, r.Signal, err), "", fmt.Errorf("kill(%d, %s): %w", pid, r.Signal, err)
	}
	return fmt.Sprintf("sent %s to pid %d", r.Signal, pid), "", nil
}

func runArgv(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	// Put the child in its own process group so a timeout-kill propagates to
	// any grandchildren (e.g. a launchctl that forks, an exec script that
	// spawns helpers). Mirrors runBuildBlock and runHook.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return buf.String(), err
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return buf.String(), ctx.Err()
	}
}

// scheduleSelfExit fires os.Exit(code) from a deferred goroutine so the
// MCP handler returns first and the kernel has time to ACK the response.
// Tests override exitFunc to capture the requested code without dying.
var selfExitDelay = 100 * time.Millisecond

func scheduleSelfExit(code int) {
	// Snapshot the indirected values so a test-cleanup-time reset of
	// `exitFunc`/`selfExitDelay` doesn't race against the goroutine that
	// is about to read them.
	delay := selfExitDelay
	fn := exitFunc
	go func() {
		time.Sleep(delay)
		fn(code)
	}()
}

func writeAudit(e AuditEntry) {
	AppendAuditBestEffort(e, slog.Default())
}

func joinValidationErrors(errs []config.ValidationError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}
