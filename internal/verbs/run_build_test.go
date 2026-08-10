package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// writeExecScript writes body as an executable script at path, creating
// parent dirs as needed.
func writeExecScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestRunBuild_HappyPathScript(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "build-script-happy")
	writeExecScript(t, filepath.Join(wt, "script", "iris-build"),
		"#!/usr/bin/env bash\necho \"built-from-script $1\"\n")
	client := stubArgus(t, src, wt)

	result, err := RunBuild(context.Background(), client, "task-build", RunBuildOptions{})
	if err != nil {
		t.Fatalf("run build: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Output, "built-from-script") {
		t.Fatalf("expected output to contain 'built-from-script', got: %q", result.Output)
	}
	if !strings.Contains(result.Command, "script/iris-build") {
		t.Fatalf("expected Command to name script/iris-build, got: %q", result.Command)
	}
}

func TestRunBuild_HappyPathMakefile(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "build-make-happy")
	// Tab-indented recipe is required by make.
	makefile := "build:\n\t@echo \"built-from-make\"\n"
	if err := os.WriteFile(filepath.Join(wt, "Makefile"), []byte(makefile), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}
	client := stubArgus(t, src, wt)

	result, err := RunBuild(context.Background(), client, "task-make", RunBuildOptions{})
	if err != nil {
		t.Fatalf("run build: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0, got %d (output: %q)", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "built-from-make") {
		t.Fatalf("expected output to contain 'built-from-make', got: %q", result.Output)
	}
	if !strings.Contains(result.Command, "make build") {
		t.Fatalf("expected Command to name 'make build', got: %q", result.Command)
	}
}

func TestRunBuild_NoBuildMechanism(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "build-none")
	client := stubArgus(t, src, wt)

	_, err := RunBuild(context.Background(), client, "task-none", RunBuildOptions{})
	if err == nil {
		t.Fatal("expected error when no build mechanism present, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "script/iris-build") {
		t.Fatalf("expected error to name script/iris-build, got: %v", err)
	}
	if !strings.Contains(msg, "Makefile") {
		t.Fatalf("expected error to name Makefile, got: %v", err)
	}
}

func TestRunBuild_NonZeroExit(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "build-fail")
	writeExecScript(t, filepath.Join(wt, "script", "iris-build"),
		"#!/usr/bin/env bash\necho \"oops something broke\"\nexit 1\n")
	client := stubArgus(t, src, wt)

	result, err := RunBuild(context.Background(), client, "task-fail", RunBuildOptions{})
	if err == nil {
		t.Fatal("expected error for non-zero exit, got nil")
	}

	// errors.As must yield a *BuildExitError carrying the populated result.
	var buildErr *BuildExitError
	if !errors.As(err, &buildErr) {
		t.Fatalf("expected *BuildExitError via errors.As, got %T: %v", err, err)
	}
	if buildErr.Result == nil {
		t.Fatal("BuildExitError.Result is nil; expected populated *RunBuildResult")
	}
	if buildErr.Result.ExitCode != 1 {
		t.Fatalf("expected ExitCode=1, got %d", buildErr.Result.ExitCode)
	}
	if !strings.Contains(buildErr.Result.Output, "oops something broke") {
		t.Fatalf("expected Output to contain script echo, got: %q", buildErr.Result.Output)
	}

	// The verb also returns the result directly so callers don't have to
	// pry it out of the error if they don't want to.
	if result == nil {
		t.Fatal("expected non-nil result alongside *BuildExitError")
	}
	if result.ExitCode != 1 {
		t.Fatalf("expected result.ExitCode=1, got %d", result.ExitCode)
	}
}

func TestRunBuild_TargetArgument(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "build-target")
	writeExecScript(t, filepath.Join(wt, "script", "iris-build"),
		"#!/usr/bin/env bash\necho \"target-was $1\"\n")
	client := stubArgus(t, src, wt)

	result, err := RunBuild(context.Background(), client, "task-target",
		RunBuildOptions{Target: "release"})
	if err != nil {
		t.Fatalf("run build: %v", err)
	}
	if !strings.Contains(result.Output, "target-was release") {
		t.Fatalf("expected output to echo 'target-was release', got: %q", result.Output)
	}
	if !strings.Contains(result.Command, "release") {
		t.Fatalf("expected Command to include target, got: %q", result.Command)
	}
}

// Delta scenario ("Resolved secrets reach the build subprocess via
// script/iris-build"): a resolvable [secrets.env] mapping in
// .iris.local.toml on the resolved SOURCE REPO reaches the script/iris-build
// subprocess's own environment.
func TestRunBuild_SecretsInjectedScript(t *testing.T) {
	// No t.Parallel(): t.Setenv forbids it.
	src, wt := setupRepoWithWorktree(t, "build-secrets-script")
	t.Setenv("IRIS_TEST_SECRET_RUNBUILD_SCRIPT_SRC", "resolved-script-value")
	writeFileT(t, src, config.IrisTomlFilename, "schema_version = 1\n")
	writeFileT(t, src, config.IrisLocalTomlFilename, `
[secrets.env]
IRIS_TEST_SECRET_RUNBUILD_SCRIPT = "env://IRIS_TEST_SECRET_RUNBUILD_SCRIPT_SRC"
`)
	writeExecScript(t, filepath.Join(wt, "script", "iris-build"),
		"#!/usr/bin/env bash\necho \"secret-is $IRIS_TEST_SECRET_RUNBUILD_SCRIPT\"\n")
	client := stubArgus(t, src, wt)

	result, err := RunBuild(context.Background(), client, "task-build-secrets-script", RunBuildOptions{})
	if err != nil {
		t.Fatalf("run build: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0, got %d (output: %q)", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "secret-is resolved-script-value") {
		t.Fatalf("expected resolved secret in output, got: %q", result.Output)
	}
}

// Delta scenario ("Resolved secrets reach the build subprocess via the
// Makefile fallback"): same shape as above, but through `make build`.
func TestRunBuild_SecretsInjectedMakefile(t *testing.T) {
	// No t.Parallel(): t.Setenv forbids it.
	src, wt := setupRepoWithWorktree(t, "build-secrets-make")
	t.Setenv("IRIS_TEST_SECRET_RUNBUILD_MAKE_SRC", "resolved-make-value")
	writeFileT(t, src, config.IrisTomlFilename, "schema_version = 1\n")
	writeFileT(t, src, config.IrisLocalTomlFilename, `
[secrets.env]
IRIS_TEST_SECRET_RUNBUILD_MAKE = "env://IRIS_TEST_SECRET_RUNBUILD_MAKE_SRC"
`)
	// Tab-indented recipe is required by make. "$$" escapes to a literal
	// "$" so make hands the shell "$IRIS_TEST_SECRET_RUNBUILD_MAKE".
	makefile := "build:\n\t@echo \"secret-is $$IRIS_TEST_SECRET_RUNBUILD_MAKE\"\n"
	if err := os.WriteFile(filepath.Join(wt, "Makefile"), []byte(makefile), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}
	client := stubArgus(t, src, wt)

	result, err := RunBuild(context.Background(), client, "task-build-secrets-make", RunBuildOptions{})
	if err != nil {
		t.Fatalf("run build: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0, got %d (output: %q)", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "secret-is resolved-make-value") {
		t.Fatalf("expected resolved secret in output, got: %q", result.Output)
	}
}

// Delta scenario ("No config present changes nothing"): no .iris.toml/
// .iris.local.toml anywhere is a full no-op, matching pre-existing behavior
// exactly.
func TestRunBuild_NoConfigIsNoOp(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "build-secrets-noconfig")
	writeExecScript(t, filepath.Join(wt, "script", "iris-build"),
		"#!/usr/bin/env bash\necho built-no-config\n")
	client := stubArgus(t, src, wt)

	result, err := RunBuild(context.Background(), client, "task-build-noconfig", RunBuildOptions{})
	if err != nil {
		t.Fatalf("run build: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0, got %d (output: %q)", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "built-no-config") {
		t.Fatalf("expected output to contain 'built-no-config', got: %q", result.Output)
	}
}

// Delta scenario ("An unresolved secret does not block the build"): a
// [secrets.env] mapping whose source fails to resolve leaves that one
// target variable unset and does not fail the build.
func TestRunBuild_UnresolvedSecretDoesNotBlockBuild(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "build-secrets-unresolved")
	writeFileT(t, src, config.IrisTomlFilename, "schema_version = 1\n")
	writeFileT(t, src, config.IrisLocalTomlFilename, `
[secrets.env]
IRIS_TEST_SECRET_RUNBUILD_UNRESOLVED = "env://IRIS_TEST_SECRET_RUNBUILD_UNRESOLVED_SRC_DOES_NOT_EXIST"
`)
	writeExecScript(t, filepath.Join(wt, "script", "iris-build"),
		"#!/usr/bin/env bash\necho \"secret-was [$IRIS_TEST_SECRET_RUNBUILD_UNRESOLVED]\"\n")
	client := stubArgus(t, src, wt)

	result, err := RunBuild(context.Background(), client, "task-build-unresolved", RunBuildOptions{})
	if err != nil {
		t.Fatalf("run build: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0 (unresolved secret must not block the build), got %d (output: %q)", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "secret-was []") {
		t.Fatalf("expected target var to be left unset (empty), got: %q", result.Output)
	}
}

// Concurrent builds on DIFFERENT worktrees of the same source repo must
// run in parallel — the per-worktree lock is keyed on worktree path,
// not source repo path.
//
// The test measures both serial and parallel wall time and asserts that
// parallel is materially faster than serial. This is robust to per-test
// setup overhead (Resolve does git + HTTP calls that can add 100-300ms
// on a loaded machine), unlike a fixed wall-time threshold.
func TestRunBuild_ConcurrentDifferentWorktreesParallel(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	wt1 := filepath.Join(tmp, "wt1")
	wt2 := filepath.Join(tmp, "wt2")
	g := gitRunner(t)

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "x@y.z")
	g(src, "config", "user.name", "x")
	g(src, "commit", "--allow-empty", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")
	g(src, "branch", "argus/concurrent-a")
	g(src, "branch", "argus/concurrent-b")
	g(src, "worktree", "add", wt1, "argus/concurrent-a")
	g(src, "worktree", "add", wt2, "argus/concurrent-b")

	// 400ms sleep is long enough that build dominates the per-call setup
	// overhead, so the parallel-vs-serial wall-time ratio is clear.
	const buildSleep = 400 * time.Millisecond
	const sleepBody = "#!/usr/bin/env bash\nsleep 0.4\necho done\n"
	writeExecScript(t, filepath.Join(wt1, "script", "iris-build"), sleepBody)
	writeExecScript(t, filepath.Join(wt2, "script", "iris-build"), sleepBody)

	canon, _ := filepath.EvalSymlinks(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/tasks/task-a":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-a", "worktree_path": wt1})
		case r.URL.Path == "/api/tasks/task-b":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-b", "worktree_path": wt2})
		case r.URL.Path == "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client := argus.New(srv.URL, "stub-token")

	// Baseline: two SERIAL builds. Wall time = ~2*sleep + 2*setup.
	serialStart := time.Now()
	if _, err := RunBuild(context.Background(), client, "task-a", RunBuildOptions{}); err != nil {
		t.Fatalf("serial build a: %v", err)
	}
	if _, err := RunBuild(context.Background(), client, "task-b", RunBuildOptions{}); err != nil {
		t.Fatalf("serial build b: %v", err)
	}
	serialElapsed := time.Since(serialStart)

	// Parallel: two builds concurrently. Wall time = ~sleep + max(setup_a, setup_b).
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	parallelStart := time.Now()
	go func() {
		defer wg.Done()
		_, errs[0] = RunBuild(context.Background(), client, "task-a", RunBuildOptions{})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = RunBuild(context.Background(), client, "task-b", RunBuildOptions{})
	}()
	wg.Wait()
	parallelElapsed := time.Since(parallelStart)

	for i, err := range errs {
		if err != nil {
			t.Fatalf("parallel build %d: %v", i, err)
		}
	}

	// Parallel must be materially faster than serial. If per-worktree
	// locking was broken and they serialized through `repoLocks`, the
	// parallel time would be roughly equal to serial. We require parallel
	// to save at least one full sleep duration's worth of time.
	saved := serialElapsed - parallelElapsed
	if saved < buildSleep-100*time.Millisecond {
		t.Fatalf("expected concurrent builds in different worktrees to run in parallel; serial=%v parallel=%v saved=%v (must save >= %v)",
			serialElapsed, parallelElapsed, saved, buildSleep-100*time.Millisecond)
	}
}
