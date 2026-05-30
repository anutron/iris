package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// victim is a long-running child process the signal-mechanism test sends
// SIGTERM to.
type victim struct {
	cmd *exec.Cmd
}

func (v *victim) cleanup() {
	if v.cmd != nil && v.cmd.Process != nil {
		_ = v.cmd.Process.Kill()
		_, _ = v.cmd.Process.Wait()
	}
}

func (v *victim) alive() bool {
	if v.cmd == nil || v.cmd.Process == nil {
		return false
	}
	// signal 0 probes liveness without affecting the process.
	return syscall.Kill(v.cmd.Process.Pid, syscall.Signal(0)) == nil
}

func startVictimSleep(t *testing.T, pidFile string) *victim {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start victim: %v", err)
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("write pid file: %v", err)
	}
	return &victim{cmd: cmd}
}

// reloadFixture sets up:
//   - bare origin, source clone on `main`, origin/HEAD set
//   - a `.iris.toml` with the given body
//   - a fake `bin/iris` so ResolveSelf returns this repo when isSelf=true
//   - audit log redirected to a tmp dir
//   - the executable() override so ResolveSelf points at the fixture
//   - a stub argus that allowlists the canonical source repo
//
// Returns the source repo path, the stub argus client, and a cleanup func
// (the cleanup is registered with t.Cleanup automatically).
func reloadFixture(t *testing.T, body string, isSelf bool) (src string, client *argus.Client) {
	t.Helper()
	setAuditDir(t)

	src = setupRepoOnly(t)
	// Put a fake bin/iris in the source repo so isSelfTarget() detection
	// works when isSelf=true. Even when isSelf=false we still want
	// ResolveSelf to point somewhere — we use a separate tmp dir.
	bin := filepath.Join(src, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("x"), 0o755)
	old := executable
	if isSelf {
		executable = func() (string, error) { return bin, nil }
	} else {
		// ResolveSelf must resolve elsewhere — a separate repo so the
		// canonical-path comparison treats this fixture as cross-target.
		elsewhereSrc := setupRepoOnly(t)
		elseBin := filepath.Join(elsewhereSrc, "bin", "iris")
		_ = os.MkdirAll(filepath.Dir(elseBin), 0o755)
		_ = os.WriteFile(elseBin, []byte("x"), 0o755)
		executable = func() (string, error) { return elseBin, nil }
	}
	t.Cleanup(func() { executable = old })

	tomlPath := filepath.Join(src, ".iris.toml")
	if err := os.WriteFile(tomlPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write .iris.toml: %v", err)
	}
	// Commit .iris.toml + bin/iris so the working tree is clean for the
	// pre-flight refusal check. Otherwise every test trips on dirty tree.
	// Also push: tests that simulate origin advancing need a baseline
	// where origin == source for the ff-only merge to succeed.
	g := gitRunner(t)
	g(src, "add", ".iris.toml", "bin/iris")
	g(src, "commit", "-m", "fixture: .iris.toml + bin")
	g(src, "push", "origin", "main")

	canon, _ := filepath.EvalSymlinks(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/projects/full" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/tasks/") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": strings.TrimPrefix(r.URL.Path, "/api/tasks/"), "worktree_path": src,
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	client = argus.New(srv.URL, "stub-token")

	// Capture exit code instead of dying.
	atomic.StoreInt32(&capturedExitCode, -1)
	exitFunc = func(code int) { atomic.StoreInt32(&capturedExitCode, int32(code)) }
	selfExitDelay = 10 * time.Millisecond
	t.Cleanup(func() {
		exitFunc = os.Exit
		selfExitDelay = 100 * time.Millisecond
	})
	return src, client
}

var capturedExitCode int32 = -1

// waitForExit polls the capturedExitCode for up to d.
func waitForExit(t *testing.T, d time.Duration) int32 {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if c := atomic.LoadInt32(&capturedExitCode); c >= 0 {
			return c
		}
		time.Sleep(5 * time.Millisecond)
	}
	return atomic.LoadInt32(&capturedExitCode)
}

const tomlSelfExitCode = `
schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "exit_code"
`

const tomlCrossNone = `
schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "none"
`

// --- Pre-flight refusals ----------------------------------------------------

func TestReload_RefusesDirtyTree(t *testing.T) {
	src, client := reloadFixture(t, tomlSelfExitCode, true)
	if err := os.WriteFile(filepath.Join(src, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirt: %v", err)
	}
	_, err := Reload(context.Background(), client, ReloadInput{NoPull: true})
	if err == nil {
		t.Fatal("expected dirty-tree refusal")
	}
	if !strings.Contains(err.Error(), "dirty") && !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReload_RefusesNonDefaultBranch(t *testing.T) {
	src, client := reloadFixture(t, tomlSelfExitCode, true)
	g := gitRunner(t)
	g(src, "switch", "-c", "side-branch")
	_, err := Reload(context.Background(), client, ReloadInput{NoPull: true})
	if err == nil {
		t.Fatal("expected branch refusal")
	}
	if !strings.Contains(err.Error(), "side-branch") && !strings.Contains(err.Error(), "main") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReload_RefusesMissingIrisToml(t *testing.T) {
	// Use a fixture targeting a repo with no .iris.toml.
	setAuditDir(t)
	src, client := reloadFixtureFresh(t)
	// Make ResolveSelf point elsewhere so this is treated as cross-target.
	elsewhereSrc := setupRepoOnly(t)
	elseBin := filepath.Join(elsewhereSrc, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(elseBin), 0o755)
	_ = os.WriteFile(elseBin, []byte("x"), 0o755)
	old := executable
	executable = func() (string, error) { return elseBin, nil }
	t.Cleanup(func() { executable = old })

	_, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Path: src})
	if err == nil {
		t.Fatal("expected missing-toml refusal")
	}
	if !strings.Contains(err.Error(), ".iris.toml") {
		t.Fatalf("expected error to mention .iris.toml, got: %v", err)
	}
}

// reloadFixtureFresh is a minimal source-only fixture (no .iris.toml) for
// tests that need to assert missing-file behavior.
func reloadFixtureFresh(t *testing.T) (string, *argus.Client) {
	t.Helper()
	src := setupRepoOnly(t)
	canon, _ := filepath.EvalSymlinks(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/projects/full" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return src, argus.New(srv.URL, "stub-token")
}

func TestReload_RefusesUnknownSchemaVersion(t *testing.T) {
	_, client := reloadFixture(t, `schema_version = 99
[build]
command = ["true"]
[restart]
mechanism = "none"
`, true)
	_, err := Reload(context.Background(), client, ReloadInput{NoPull: true})
	if err == nil {
		t.Fatal("expected schema_version refusal")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReload_RefusesExitCodeOnCrossTarget(t *testing.T) {
	src, client := reloadFixture(t, tomlSelfExitCode, false)
	_, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Path: src})
	if err == nil {
		t.Fatal("expected exit_code-on-cross refusal")
	}
	if !strings.Contains(err.Error(), "self-only") && !strings.Contains(err.Error(), "exit_code") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReload_RefusesCrossMechanismFieldMismatch(t *testing.T) {
	src, client := reloadFixture(t, `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "launchagent"
label = "com.example.x"
pid_file = "/tmp/foo.pid"
`, false)
	_, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Path: src})
	if err == nil {
		t.Fatal("expected field-mismatch refusal")
	}
	if !strings.Contains(err.Error(), "pid_file") {
		t.Fatalf("expected pid_file in error, got: %v", err)
	}
}

// --- Pull behavior ----------------------------------------------------------

func TestReload_NoPullSkipsFetch(t *testing.T) {
	_, client := reloadFixture(t, tomlSelfExitCode, true)
	// Caller "self" (not "cli") so the CLI-self-reload refusal does not fire;
	// this test exercises the self-reload pull-skip path under MCP.
	res, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Caller: "self"})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if res.Pulled {
		t.Fatalf("expected Pulled=false with NoPull, got true")
	}
	if res.PrePullSha != res.PostPullSha {
		t.Fatalf("expected pre==post sha with NoPull, got %s vs %s", res.PrePullSha, res.PostPullSha)
	}
}

func TestReload_DefaultPullsFastForward(t *testing.T) {
	src, client := reloadFixture(t, tomlSelfExitCode, true)
	// Advance origin/main so a pull is observable.
	g := gitRunner(t)
	other := t.TempDir()
	g("", "clone", filepath.Join(filepath.Dir(src), "origin.git"), other)
	g(other, "config", "user.email", "x@y.z")
	g(other, "config", "user.name", "x")
	g(other, "commit", "--allow-empty", "-m", "new")
	g(other, "push", "origin", "main")

	// Caller "self" — exercise the self-reload fast-forward path via MCP.
	res, err := Reload(context.Background(), client, ReloadInput{Caller: "self"})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !res.Pulled {
		t.Fatal("expected Pulled=true")
	}
	if res.PrePullSha == res.PostPullSha {
		t.Fatalf("expected pre != post after pull")
	}
}

func TestReload_RefusesDivergentHistory(t *testing.T) {
	src, client := reloadFixture(t, tomlSelfExitCode, true)
	g := gitRunner(t)
	// Local commit on main.
	g(src, "config", "user.email", "x@y.z")
	g(src, "config", "user.name", "x")
	g(src, "commit", "--allow-empty", "-m", "local")
	// Origin diverged: another clone pushes its own commit.
	other := t.TempDir()
	g("", "clone", filepath.Join(filepath.Dir(src), "origin.git"), other)
	g(other, "config", "user.email", "x@y.z")
	g(other, "config", "user.name", "x")
	g(other, "commit", "--allow-empty", "-m", "origin-side")
	g(other, "push", "origin", "main")

	// Caller "self" — exercise the self-reload divergent-history refusal
	// via MCP (the CLI-self-reload refusal would short-circuit otherwise).
	_, err := Reload(context.Background(), client, ReloadInput{Caller: "self"})
	if err == nil {
		t.Fatal("expected divergent-history refusal")
	}
	if !strings.Contains(err.Error(), "fast-forward") {
		t.Fatalf("expected ff error, got: %v", err)
	}
}

// --- Pull-then-validate (forward-compatible ordering) -----------------------

// advanceOriginToml pushes a new `.iris.toml` body onto origin/main via a
// throwaway clone, so the fixture's source repo is one fast-forward behind a
// changed config — the setup for post-pull validation tests.
func advanceOriginToml(t *testing.T, src, body string) {
	t.Helper()
	g := gitRunner(t)
	other := t.TempDir()
	g("", "clone", filepath.Join(filepath.Dir(src), "origin.git"), other)
	g(other, "config", "user.email", "x@y.z")
	g(other, "config", "user.name", "x")
	if err := os.WriteFile(filepath.Join(other, ".iris.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write advanced toml: %v", err)
	}
	g(other, "add", ".iris.toml")
	g(other, "commit", "-m", "advance .iris.toml")
	g(other, "push", "origin", "main")
}

// Headline regression: an additive field that only exists on origin is pulled
// in, tolerated as a warning, and the reload proceeds to build/restart. This is
// the one-shot additive-deploy property the change exists to provide.
func TestReload_ToleratesUnknownFieldPostPull(t *testing.T) {
	src, client := reloadFixture(t, tomlCrossNone, false)
	advanceOriginToml(t, src, `schema_version = 1
future_field = "x"
[build]
command = ["true"]
[restart]
mechanism = "none"
`)
	res, err := Reload(context.Background(), client, ReloadInput{Path: src, Caller: "cli"})
	if err != nil {
		t.Fatalf("reload should tolerate unknown post-pull field, got: %v", err)
	}
	if !res.Pulled {
		t.Fatal("expected Pulled=true")
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "future_field") {
		t.Fatalf("expected warning naming future_field, got: %v", res.Warnings)
	}
}

// Validation runs against the POST-pull config: a config that is valid pre-pull
// but invalid on origin must fail the reload (proving we validate the truth
// that will run, not stale on-disk state).
func TestReload_RefusesInvalidPostPullConfig(t *testing.T) {
	src, client := reloadFixture(t, tomlCrossNone, false)
	advanceOriginToml(t, src, `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "launchagent"
label = "com.example.x"
pid_file = "/tmp/foo.pid"
`)
	_, err := Reload(context.Background(), client, ReloadInput{Path: src, Caller: "cli"})
	if err == nil {
		t.Fatal("expected post-pull validation refusal")
	}
	if !strings.Contains(err.Error(), "pid_file") {
		t.Fatalf("expected pid_file error from post-pull config, got: %v", err)
	}
}

// schema_version mismatch is NOT tolerated even with forward-compat decoding.
func TestReload_SchemaVersionStillFailsPostPull(t *testing.T) {
	src, client := reloadFixture(t, tomlCrossNone, false)
	advanceOriginToml(t, src, `schema_version = 99
future_field = "x"
[build]
command = ["true"]
[restart]
mechanism = "none"
`)
	_, err := Reload(context.Background(), client, ReloadInput{Path: src, Caller: "cli"})
	if err == nil {
		t.Fatal("expected schema_version refusal post-pull")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("expected schema_version error, got: %v", err)
	}
}

// A malformed on-disk `.iris.toml` that origin fixes must NOT block the pull:
// the lenient pre-pull peek falls back to git's origin/HEAD, the pull brings
// the fix, and post-pull validation passes.
func TestReload_MalformedPrePullDoesNotBlockPull(t *testing.T) {
	src, client := reloadFixture(t, "schema_version = = 1\n", false)
	advanceOriginToml(t, src, `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "none"
`)
	res, err := Reload(context.Background(), client, ReloadInput{Path: src, Caller: "cli"})
	if err != nil {
		t.Fatalf("malformed-on-disk fixed-on-origin should deploy, got: %v", err)
	}
	if !res.Pulled {
		t.Fatal("expected Pulled=true")
	}
}

// --- Build step -------------------------------------------------------------

func TestReload_BuildSuccessIncludesOutput(t *testing.T) {
	_, client := reloadFixture(t, `schema_version = 1
[build]
command = ["sh", "-c", "echo hello && echo world"]
[restart]
mechanism = "exit_code"
`, true)
	// Caller "self" — exercise self-reload build path via MCP.
	res, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Caller: "self"})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.Contains(res.BuildOutput, "hello") || !strings.Contains(res.BuildOutput, "world") {
		t.Fatalf("expected hello+world in BuildOutput, got: %q", res.BuildOutput)
	}
}

func TestReload_BuildFailureAborts(t *testing.T) {
	_, client := reloadFixture(t, `schema_version = 1
[build]
command = ["sh", "-c", "echo oops; exit 7"]
[restart]
mechanism = "exit_code"
`, true)
	// Caller "self" — exercise self-reload build-failure path via MCP.
	_, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Caller: "self"})
	if err == nil {
		t.Fatal("expected build failure")
	}
	if !strings.Contains(err.Error(), "oops") {
		t.Fatalf("expected oops in error, got: %v", err)
	}
	// Audit should record the failure.
	entries, _ := ReadAudit(AuditReadOpts{})
	if len(entries) != 1 || entries[0].Outcome != "failure" {
		t.Fatalf("expected one failure audit entry, got: %+v", entries)
	}
}

func TestReload_BuildTimeoutKillsProcess(t *testing.T) {
	_, client := reloadFixture(t, `schema_version = 1
[build]
command = ["sleep", "10"]
timeout_seconds = 1
[restart]
mechanism = "exit_code"
`, true)
	start := time.Now()
	// Caller "self" — exercise self-reload build-timeout path via MCP.
	_, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Caller: "self"})
	if err == nil {
		t.Fatal("expected build timeout")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout did not kill build promptly: %v", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected 'timed out' in error, got: %v", err)
	}
}

// --- Pre-flight hook --------------------------------------------------------

func TestReload_PreFlightHookAborts(t *testing.T) {
	_, client := reloadFixture(t, `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "exit_code"
[pre_flight]
command = ["sh", "-c", "echo blocker; exit 1"]
`, true)
	// Caller "self" — exercise self-reload pre-flight-hook path via MCP.
	_, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Caller: "self"})
	if err == nil {
		t.Fatal("expected pre-flight refusal")
	}
	if !strings.Contains(err.Error(), "blocker") {
		t.Fatalf("expected hook output in error: %v", err)
	}
}

// --- Restart dispatch -------------------------------------------------------

func TestReload_ExitCodeSchedulesExit(t *testing.T) {
	_, client := reloadFixture(t, `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "exit_code"
code = 42
`, true)
	// Caller "self" — exercise the self-reload exit_code dispatch via MCP
	// (CLI self-reload is refused at pre-flight).
	res, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Caller: "self"})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if res.Mode != "self" {
		t.Fatalf("expected mode=self, got %q", res.Mode)
	}
	if !res.RestartPending {
		t.Fatalf("expected RestartPending=true for self-reload")
	}
	if got := waitForExit(t, 2*time.Second); got != 42 {
		t.Fatalf("expected exit code 42, got %d", got)
	}
}

func TestReload_NoneMechanismDoesNothing(t *testing.T) {
	src, client := reloadFixture(t, tomlCrossNone, false)
	res, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Path: src, Caller: "cli"})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if res.Mode != "cross" {
		t.Fatalf("expected mode=cross, got %q", res.Mode)
	}
	if res.RestartOutput != "" {
		t.Fatalf("expected empty RestartOutput for mechanism=none, got %q", res.RestartOutput)
	}
	if res.RestartPending {
		t.Fatalf("expected RestartPending=false for cross-reload")
	}
}

func TestReload_ExecMechanismRunsArgv(t *testing.T) {
	src, client := reloadFixture(t, `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "exec"
command = ["echo", "restarted-via-exec"]
`, false)
	res, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Path: src, Caller: "cli"})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.Contains(res.RestartOutput, "restarted-via-exec") {
		t.Fatalf("expected exec output captured, got: %q", res.RestartOutput)
	}
}

func TestReload_SignalMechanismSendsSignal(t *testing.T) {
	// Start a long-running sleep, write its PID, then reload with signal mechanism.
	pidFile := filepath.Join(t.TempDir(), "victim.pid")
	cmd := startVictimSleep(t, pidFile)
	defer cmd.cleanup()

	src, client := reloadFixture(t, fmt.Sprintf(`schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "signal"
pid_file = "%s"
signal = "SIGTERM"
`, pidFile), false)
	res, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Path: src, Caller: "cli"})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.Contains(res.RestartOutput, "SIGTERM") {
		t.Fatalf("expected SIGTERM in restart output, got: %q", res.RestartOutput)
	}
	// Reap the victim so it doesn't show as alive (zombie). Then assert it
	// actually terminated via SIGTERM (signal exit, not killed by Go's kill).
	done := make(chan error, 1)
	go func() { done <- cmd.cmd.Wait() }()
	select {
	case <-done:
		// died; that's the success path
	case <-time.After(3 * time.Second):
		t.Fatal("victim process did not exit after SIGTERM within 3s")
	}
}

// --- Audit log -------------------------------------------------------------

func TestReload_AuditWrittenOnSuccessAndFailure(t *testing.T) {
	src, client := reloadFixture(t, tomlCrossNone, false)
	if _, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Path: src, Caller: "cli"}); err != nil {
		t.Fatalf("reload 1: %v", err)
	}
	// Failure: rewrite to a bad build, commit it.
	tomlPath := filepath.Join(src, ".iris.toml")
	_ = os.WriteFile(tomlPath, []byte(`schema_version = 1
[build]
command = ["sh", "-c", "exit 1"]
[restart]
mechanism = "none"
`), 0o644)
	g := gitRunner(t)
	g(src, "add", ".iris.toml")
	g(src, "commit", "-m", "bad build")
	if _, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Path: src, Caller: "cli"}); err == nil {
		t.Fatal("expected failure")
	}
	entries, _ := ReadAudit(AuditReadOpts{})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Outcome != "success" {
		t.Fatalf("first should be success: %+v", entries[0])
	}
	if entries[1].Outcome != "failure" || entries[1].FailureReason == "" {
		t.Fatalf("second should be failure with reason: %+v", entries[1])
	}
}

// --- task_id optionality ---------------------------------------------------

func TestReload_AmbiguousBothInputs(t *testing.T) {
	_, client := reloadFixture(t, tomlCrossNone, false)
	_, err := Reload(context.Background(), client, ReloadInput{TaskID: "x", Path: "/p"})
	if err == nil {
		t.Fatal("expected ambiguous error")
	}
}

// --- Per-source-repo lock ---------------------------------------------------

func TestReload_LockSerializesConcurrent(t *testing.T) {
	src, client := reloadFixture(t, `schema_version = 1
[build]
command = ["sh", "-c", "sleep 0.3; echo ok"]
[restart]
mechanism = "none"
`, false)
	var wg sync.WaitGroup
	wg.Add(2)
	t0 := time.Now()
	var d1, d2 time.Duration
	go func() {
		defer wg.Done()
		s := time.Now()
		_, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Path: src, Caller: "cli"})
		if err != nil {
			t.Errorf("r1: %v", err)
		}
		d1 = time.Since(s)
	}()
	go func() {
		defer wg.Done()
		s := time.Now()
		_, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Path: src, Caller: "cli"})
		if err != nil {
			t.Errorf("r2: %v", err)
		}
		d2 = time.Since(s)
	}()
	wg.Wait()
	total := time.Since(t0)
	if total < 500*time.Millisecond {
		t.Fatalf("expected serialization to extend wall time; total=%v d1=%v d2=%v", total, d1, d2)
	}
}

func TestReload_PathAndAllowlistEnforcedForCross(t *testing.T) {
	// Build a fresh fixture with the wrong allowlist; expect a path-based
	// reload to fail at Resolve.
	_, _ = reloadFixture(t, tomlCrossNone, false)
	src := setupRepoOnly(t)
	_ = os.WriteFile(filepath.Join(src, ".iris.toml"), []byte(tomlCrossNone), 0o644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/projects/full" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "other", "path": "/some/other"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	client := argus.New(srv.URL, "stub-token")

	_, err := Reload(context.Background(), client, ReloadInput{NoPull: true, Path: src, Caller: "cli"})
	if err == nil {
		t.Fatal("expected allowlist refusal for path-resolved cross-target")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("expected allowlist error, got: %v", err)
	}
}

// --- CLI self-reload refusal (refuse-cli-self-reload) ----------------------

// tomlSelfBuildSentinel is a self-reload .iris.toml whose build emits a
// sentinel string. The refused-case tests assert this sentinel never
// appears in any audit entry's BuildOutput — proving the build never ran.
const tomlSelfBuildSentinel = `
schema_version = 1
[build]
command = ["sh", "-c", "echo BUILD_RAN_SENTINEL"]
[restart]
mechanism = "exit_code"
`

// assertCLISelfReloadRefused encapsulates the four assertions shared by the
// no-arg / explicit-path / task_id refused-case tests:
//   - the returned error is non-nil and carries the stable token
//   - the audit log contains exactly one entry, outcome=failure with the token
//   - no audit entry was ever written with outcome=success
//   - the build sentinel never reached any BuildOutput (build did not run)
//   - PrePullSha is empty on every audit entry (rev-parse / fetch did not run)
func assertCLISelfReloadRefused(t *testing.T, err error) {
	t.Helper()
	const token = "cli-self-reload-not-supported"
	if err == nil {
		t.Fatal("expected refusal error, got nil")
	}
	if !strings.Contains(err.Error(), token) {
		t.Fatalf("expected error to contain %q, got: %v", token, err)
	}
	entries, _ := ReadAudit(AuditReadOpts{})
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 audit entry, got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Outcome != "failure" {
		t.Fatalf("expected audit outcome=failure, got %q (entry: %+v)", e.Outcome, e)
	}
	if !strings.Contains(e.FailureReason, token) {
		t.Fatalf("expected audit failure_reason to contain %q, got %q", token, e.FailureReason)
	}
	for _, ent := range entries {
		if ent.Outcome == "success" {
			t.Fatalf("expected NO success audit entry; got: %+v", ent)
		}
		if strings.Contains(ent.BuildOutput, "BUILD_RAN_SENTINEL") {
			t.Fatalf("build ran despite refusal: BuildOutput=%q", ent.BuildOutput)
		}
		if ent.PrePullSha != "" || ent.PostPullSha != "" {
			t.Fatalf("git rev-parse / fetch ran despite refusal: pre=%q post=%q",
				ent.PrePullSha, ent.PostPullSha)
		}
	}
}

func TestReload_CLISelfNoArg_Refused(t *testing.T) {
	_, client := reloadFixture(t, tomlSelfBuildSentinel, true)
	// No TaskID, no Path — Reload falls through to ResolveSelf and the
	// target is the faked self source repo.
	_, err := Reload(context.Background(), client, ReloadInput{
		NoPull: true,
		Caller: "cli",
	})
	assertCLISelfReloadRefused(t, err)
}

func TestReload_CLISelfExplicitPath_Refused(t *testing.T) {
	src, client := reloadFixture(t, tomlSelfBuildSentinel, true)
	// Explicit Path that resolves to the faked self source repo.
	_, err := Reload(context.Background(), client, ReloadInput{
		NoPull: true,
		Caller: "cli",
		Path:   src,
	})
	assertCLISelfReloadRefused(t, err)
}

func TestReload_CLISelfTaskID_Refused(t *testing.T) {
	_, client := reloadFixture(t, tomlSelfBuildSentinel, true)
	// TaskID pointing at the fake argus task whose worktree_path is the
	// faked self source repo (see reloadFixture's httptest stub).
	_, err := Reload(context.Background(), client, ReloadInput{
		NoPull: true,
		Caller: "cli",
		TaskID: "fake-task-self",
	})
	assertCLISelfReloadRefused(t, err)
}

// TestReload_MCPSelfUnaffected confirms the refusal does NOT fire when the
// caller is the MCP entry point (Caller != "cli"). The existing self-reload
// happy path must run end-to-end, including scheduleSelfExit.
func TestReload_MCPSelfUnaffected(t *testing.T) {
	_, client := reloadFixture(t, `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "exit_code"
code = 75
`, true)
	res, err := Reload(context.Background(), client, ReloadInput{
		NoPull: true,
		Caller: "self",
	})
	if err != nil {
		t.Fatalf("MCP self-reload returned error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result for MCP self-reload")
	}
	if res.Mode != "self" {
		t.Fatalf("expected mode=self, got %q", res.Mode)
	}
	if !res.RestartPending {
		t.Fatal("expected RestartPending=true for MCP self-reload")
	}
	if got := waitForExit(t, 2*time.Second); got != 75 {
		t.Fatalf("expected scheduled exit code 75, got %d", got)
	}
	entries, _ := ReadAudit(AuditReadOpts{})
	if len(entries) != 1 || entries[0].Outcome != "success" {
		t.Fatalf("expected one success audit entry, got: %+v", entries)
	}
	if strings.Contains(entries[0].FailureReason, "cli-self-reload-not-supported") {
		t.Fatalf("MCP self-reload incorrectly tripped CLI refusal: %+v", entries[0])
	}
}

// TestReload_CLICrossUnaffected confirms the refusal does NOT fire when the
// CLI targets a non-self repo. The full cross-reload flow runs.
func TestReload_CLICrossUnaffected(t *testing.T) {
	src, client := reloadFixture(t, tomlCrossNone, false)
	res, err := Reload(context.Background(), client, ReloadInput{
		NoPull: true,
		Path:   src,
		Caller: "cli",
	})
	if err != nil {
		t.Fatalf("CLI cross-reload returned error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result for CLI cross-reload")
	}
	if res.Mode != "cross" {
		t.Fatalf("expected mode=cross, got %q", res.Mode)
	}
	entries, _ := ReadAudit(AuditReadOpts{})
	if len(entries) != 1 || entries[0].Outcome != "success" {
		t.Fatalf("expected one success audit entry, got: %+v", entries)
	}
	if strings.Contains(entries[0].FailureReason, "cli-self-reload-not-supported") {
		t.Fatalf("CLI cross-reload incorrectly tripped CLI refusal: %+v", entries[0])
	}
}

func TestJoinValidationErrors(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		if got := joinValidationErrors(nil); got != "" {
			t.Fatalf("expected empty string for nil input, got %q", got)
		}
	})
	t.Run("single error", func(t *testing.T) {
		got := joinValidationErrors([]config.ValidationError{{Field: "build.command", Message: "missing"}})
		if got != "build.command: missing" {
			t.Fatalf("unexpected single-error format: %q", got)
		}
	})
	t.Run("multiple errors joined with semicolons", func(t *testing.T) {
		got := joinValidationErrors([]config.ValidationError{
			{Field: "build.command", Message: "missing"},
			{Field: "restart.mechanism", Message: "unknown"},
		})
		if !strings.Contains(got, "build.command: missing") {
			t.Fatalf("missing first error: %q", got)
		}
		if !strings.Contains(got, "restart.mechanism: unknown") {
			t.Fatalf("missing second error: %q", got)
		}
		if !strings.Contains(got, "; ") {
			t.Fatalf("missing separator: %q", got)
		}
	})
}
