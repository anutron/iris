package verbs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anutron/iris/internal/config"
	"github.com/anutron/iris/internal/secrets"
)

func TestRunChecks_HappyPathScript(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-script-happy")
	// Echo $1 so the test can assert the check arg is passed through.
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"),
		"#!/usr/bin/env bash\necho \"checked-from-script $1\"\n")
	client := stubArgus(t, src, wt)

	result, err := RunChecks(context.Background(), client, "task-check", RunChecksOptions{Check: "lint"})
	if err != nil {
		t.Fatalf("run checks: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Output, "checked-from-script lint") {
		t.Fatalf("expected output to echo 'checked-from-script lint', got: %q", result.Output)
	}
	if !strings.Contains(result.Command, "script/iris-check") {
		t.Fatalf("expected Command to name script/iris-check, got: %q", result.Command)
	}
	if !strings.Contains(result.Command, "lint") {
		t.Fatalf("expected Command to include the check, got: %q", result.Command)
	}
}

func TestRunChecks_NoCheckMechanism(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-none")
	client := stubArgus(t, src, wt)

	_, err := RunChecks(context.Background(), client, "task-none", RunChecksOptions{Check: "lint"})
	if err == nil {
		t.Fatal("expected error when no check script present, got nil")
	}
	if !strings.Contains(err.Error(), "script/iris-check") {
		t.Fatalf("expected error to name script/iris-check, got: %v", err)
	}
}

func TestRunChecks_NonZeroExit(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-fail")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"),
		"#!/usr/bin/env bash\necho \"lint found offenses\"\nexit 1\n")
	client := stubArgus(t, src, wt)

	result, err := RunChecks(context.Background(), client, "task-fail", RunChecksOptions{Check: "lint"})
	if err == nil {
		t.Fatal("expected error for non-zero exit, got nil")
	}

	// errors.As must yield a *CheckExitError carrying the populated result.
	var checkErr *CheckExitError
	if !errors.As(err, &checkErr) {
		t.Fatalf("expected *CheckExitError via errors.As, got %T: %v", err, err)
	}
	if checkErr.Result == nil {
		t.Fatal("CheckExitError.Result is nil; expected populated *RunChecksResult")
	}
	if checkErr.Result.ExitCode != 1 {
		t.Fatalf("expected ExitCode=1, got %d", checkErr.Result.ExitCode)
	}
	if !strings.Contains(checkErr.Result.Output, "lint found offenses") {
		t.Fatalf("expected Output to contain script echo, got: %q", checkErr.Result.Output)
	}

	// The verb also returns the result directly so callers don't have to
	// pry it out of the error if they don't want to.
	if result == nil {
		t.Fatal("expected non-nil result alongside *CheckExitError")
	}
	if result.ExitCode != 1 {
		t.Fatalf("expected result.ExitCode=1, got %d", result.ExitCode)
	}
}

func TestRunChecks_EmptyCheck(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-empty")
	client := stubArgus(t, src, wt)

	_, err := RunChecks(context.Background(), client, "task-empty", RunChecksOptions{Check: ""})
	if err == nil {
		t.Fatal("expected error when check is empty, got nil")
	}
	if !strings.Contains(err.Error(), "check is required") {
		t.Fatalf("expected error to explain check is required, got: %v", err)
	}
}

// TestRunChecks_SecretsResolveIntoCheckEnv covers the "iris:run_checks
// secrets injection" requirement's happy-path scenario: a resolvable
// [secrets.env] mapping (env:// against a var set in iris's own process
// environment) must reach script/iris-check's subprocess environment.
//
// Not t.Parallel(): t.Setenv panics if the test (or its package) has called
// t.Parallel() — see internal/verbs/main_test.go's TestMain comment for the
// established precedent in this package.
func TestRunChecks_SecretsResolveIntoCheckEnv(t *testing.T) {
	secrets.ResetMemoCache()
	t.Cleanup(secrets.ResetMemoCache)

	src, wt := setupRepoWithWorktree(t, "check-secrets-happy")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"),
		"#!/usr/bin/env bash\necho \"SECRET_SEEN=$FOO_TARGET\"\n")
	client := stubArgus(t, src, wt)

	t.Setenv("IRIS_RUNCHECKS_TEST_SOURCE", "resolved-runchecks-value")
	// [secrets] is a `.iris.local.toml`-only field, but LoadOverlay treats
	// the local file as an OVERLAY on a required shared base — a shared
	// `.iris.toml` must exist too, or the merged Doc comes back nil (see
	// LoadOverlay's own "the local file is an OVERLAY, not a fallback"
	// comment). config.LoadOverlay reads both from resolved.SourceRepo
	// (src), never resolved.WorktreePath (wt).
	writeRunChecksConfigFile(t, filepath.Join(src, config.IrisTomlFilename), "schema_version = 1\n")
	writeRunChecksConfigFile(t, filepath.Join(src, config.IrisLocalTomlFilename),
		"[secrets.env]\nFOO_TARGET = \"env://IRIS_RUNCHECKS_TEST_SOURCE\"\n")

	result, err := RunChecks(context.Background(), client, "task-check-secrets-happy", RunChecksOptions{Check: "lint"})
	if err != nil {
		t.Fatalf("run checks: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0, got %d; output: %q", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "SECRET_SEEN=resolved-runchecks-value") {
		t.Fatalf("expected resolved secret to reach check subprocess env, got output: %q", result.Output)
	}
}

// TestRunChecks_NoSecretsConfigIsNoOp covers the "absent [secrets] block is
// a full no-op" acceptance criterion: with no .iris.toml/.iris.local.toml
// at all in the source repo (the default setupRepoWithWorktree state,
// matching iris's behavior before this change), the check still runs
// successfully with no error and no injected variable.
func TestRunChecks_NoSecretsConfigIsNoOp(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-secrets-noconfig")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"),
		"#!/usr/bin/env bash\necho \"NOOP_TARGET=[$NOOP_TARGET]\"\n")
	client := stubArgus(t, src, wt)

	result, err := RunChecks(context.Background(), client, "task-check-secrets-noconfig", RunChecksOptions{Check: "lint"})
	if err != nil {
		t.Fatalf("run checks: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0, got %d; output: %q", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "NOOP_TARGET=[]") {
		t.Fatalf("expected no injected value with no config present, got output: %q", result.Output)
	}
}

// TestRunChecks_UnresolvedSecretDoesNotBlockCheck covers the "an unresolved
// secret does not block the check" scenario: a [secrets.env] mapping whose
// source fails to resolve (the env var it names is never set) must leave
// the target variable unset in the subprocess environment WITHOUT causing
// RunChecks to fail.
func TestRunChecks_UnresolvedSecretDoesNotBlockCheck(t *testing.T) {
	secrets.ResetMemoCache()
	t.Cleanup(secrets.ResetMemoCache)

	src, wt := setupRepoWithWorktree(t, "check-secrets-unresolved")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"),
		"#!/usr/bin/env bash\necho \"UNRESOLVED_TARGET=[$UNRESOLVED_TARGET]\"\n")
	client := stubArgus(t, src, wt)

	writeRunChecksConfigFile(t, filepath.Join(src, config.IrisTomlFilename), "schema_version = 1\n")
	writeRunChecksConfigFile(t, filepath.Join(src, config.IrisLocalTomlFilename),
		"[secrets.env]\nUNRESOLVED_TARGET = \"env://IRIS_RUNCHECKS_TEST_SOURCE_UNSET\"\n")

	result, err := RunChecks(context.Background(), client, "task-check-secrets-unresolved", RunChecksOptions{Check: "lint"})
	if err != nil {
		t.Fatalf("run checks: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0 despite unresolved secret, got %d; output: %q", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "UNRESOLVED_TARGET=[]") {
		t.Fatalf("expected target variable to stay unset when its source fails to resolve, got output: %q", result.Output)
	}
}

// writeRunChecksConfigFile writes body to path, creating any missing parent
// directories. Distinct from writeExecScript (run_build_test.go): this
// writes ordinary (non-executable) config files, e.g.
// .iris.toml/.iris.local.toml. Named specifically to this file (rather than
// a generic "writeFile") since internal/verbs test helpers share one
// package-level namespace across every _test.go file in the package.
func writeRunChecksConfigFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
