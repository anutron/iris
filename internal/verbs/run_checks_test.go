package verbs

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
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
