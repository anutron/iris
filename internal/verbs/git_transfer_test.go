package verbs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- test helpers: slow git transports -------------------------------------

// setSlowPreReceiveHook configures bareDir to run a pre-receive hook that
// touches markerPath, sleeps for sleepFor, then accepts the push. It also
// pins core.hooksPath to bareDir's own hooks directory: a developer machine
// may have a global `core.hooksPath` override (redirecting hook lookup away
// from the repo-local hooks dir), which would otherwise make this hook never
// fire regardless of environment.
func setSlowPreReceiveHook(t *testing.T, bareDir, markerPath string, sleepFor time.Duration) {
	t.Helper()
	hooksDir := filepath.Join(bareDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	if out, err := exec.Command("git", "-C", bareDir, "config", "core.hooksPath", hooksDir).CombinedOutput(); err != nil {
		t.Fatalf("git config core.hooksPath: %v: %s", err, out)
	}
	script := fmt.Sprintf("#!/bin/sh\ntouch '%s'\nsleep %.3f\nexit 0\n", markerPath, sleepFor.Seconds())
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-receive"), []byte(script), 0o755); err != nil {
		t.Fatalf("write pre-receive hook: %v", err)
	}
}

// setSlowUploadPack configures the "origin" remote in dir to run a wrapper
// script as its upload-pack executable. The wrapper touches markerPath,
// sleeps for sleepFor, then execs the real git-upload-pack so the fetch
// still completes normally once the sleep elapses.
func setSlowUploadPack(t *testing.T, dir, markerPath string, sleepFor time.Duration) {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "slow-upload-pack.sh")
	script := fmt.Sprintf("#!/bin/sh\ntouch '%s'\nsleep %.3f\nexec git-upload-pack \"$@\"\n", markerPath, sleepFor.Seconds())
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write upload-pack wrapper: %v", err)
	}
	if out, err := exec.Command("git", "-C", dir, "config", "remote.origin.uploadpack", scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("git config remote.origin.uploadpack: %v: %s", err, out)
	}
}

// waitForMarker polls for markerPath to exist, up to timeout. Used to
// synchronize on "the transfer has actually started on the wire" instead of
// guessing a fixed sleep, which would make the test flaky under load.
func waitForMarker(t *testing.T, markerPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(markerPath); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("marker %s did not appear within %s", markerPath, timeout)
}

// --- classifyGitFailure ------------------------------------------------

func TestClassifyGitFailure_AuthPatterns(t *testing.T) {
	t.Parallel()
	cases := []string{
		"fatal: Authentication failed for 'https://example.com/repo.git'",
		"Permission denied (publickey).\nfatal: Could not read from remote repository.",
		"remote: Invalid username or password.",
		"fatal: could not read Username for 'https://example.com': terminal prompts disabled",
		"remote: HTTP Basic: Access denied\nfatal: Authentication failed",
	}
	for _, out := range cases {
		if got := classifyGitFailure(out); got != GitTransferAuthFailure {
			t.Errorf("classifyGitFailure(%q) = %q, want %q", out, got, GitTransferAuthFailure)
		}
	}
}

func TestClassifyGitFailure_NetworkPatterns(t *testing.T) {
	t.Parallel()
	cases := []string{
		"ssh: Could not resolve hostname example.com: nodename nor servname provided",
		"ssh: connect to host example.com port 22: Connection refused",
		"fatal: unable to access 'https://example.com/repo.git/': Connection timed out",
		"fatal: unable to access 'https://example.com/repo.git/': Could not resolve host: example.com",
		"fatal: Network is unreachable",
	}
	for _, out := range cases {
		if got := classifyGitFailure(out); got != GitTransferNetworkFailure {
			t.Errorf("classifyGitFailure(%q) = %q, want %q", out, got, GitTransferNetworkFailure)
		}
	}
}

func TestClassifyGitFailure_OtherFailureDefault(t *testing.T) {
	t.Parallel()
	cases := []string{
		"! [rejected]        main -> main (non-fast-forward)\nerror: failed to push some refs",
		"fatal: 'nope' does not appear to be a git repository",
		"something entirely unrecognized happened",
	}
	for _, out := range cases {
		if got := classifyGitFailure(out); got != GitTransferOtherFailure {
			t.Errorf("classifyGitFailure(%q) = %q, want %q", out, got, GitTransferOtherFailure)
		}
	}
}

// --- GitTransferError ----------------------------------------------------

func TestGitTransferError_MessagesDistinguishReasons(t *testing.T) {
	t.Parallel()
	base := errors.New("exit status 128")
	cases := []struct {
		reason GitTransferReason
		want   string
	}{
		{GitTransferTimeout, "timeout"},
		{GitTransferAuthFailure, "auth"},
		{GitTransferNetworkFailure, "network"},
		{GitTransferOtherFailure, "other"},
	}
	for _, c := range cases {
		err := &GitTransferError{Op: "push", Reason: c.reason, Timeout: 5 * time.Second, Err: base}
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, c.want) {
			t.Errorf("GitTransferError{Reason: %q}.Error() = %q, want it to mention %q", c.reason, err.Error(), c.want)
		}
		if !strings.Contains(msg, "push") {
			t.Errorf("GitTransferError.Error() = %q, want it to mention the op %q", err.Error(), "push")
		}
	}
}

func TestGitTransferError_Unwrap(t *testing.T) {
	t.Parallel()
	base := errors.New("boom")
	err := &GitTransferError{Op: "fetch", Reason: GitTransferOtherFailure, Err: base}
	if !errors.Is(err, base) {
		t.Fatalf("errors.Is(err, base) = false, want true (Unwrap must expose the wrapped error)")
	}
}

func TestIsGitTransferTimeout(t *testing.T) {
	t.Parallel()
	timeoutErr := fmt.Errorf("wrapped: %w", &GitTransferError{Op: "push", Reason: GitTransferTimeout, Err: errors.New("x")})
	if !IsGitTransferTimeout(timeoutErr) {
		t.Fatal("IsGitTransferTimeout: want true for a wrapped timeout GitTransferError")
	}
	otherErr := fmt.Errorf("wrapped: %w", &GitTransferError{Op: "push", Reason: GitTransferOtherFailure, Err: errors.New("x")})
	if IsGitTransferTimeout(otherErr) {
		t.Fatal("IsGitTransferTimeout: want false for a non-timeout GitTransferError")
	}
	if IsGitTransferTimeout(errors.New("plain error")) {
		t.Fatal("IsGitTransferTimeout: want false for an unrelated error")
	}
}

// --- gitTransferTimeout ----------------------------------------------------

func TestGitTransferTimeout_DefaultsWhenNoConfigFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if got := gitTransferTimeout(dir); got != 300*time.Second {
		t.Fatalf("gitTransferTimeout(no config) = %s, want %s", got, 300*time.Second)
	}
}

func TestGitTransferTimeout_UsesConfiguredValue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	toml := "schema_version = 1\ngit_transfer_timeout_seconds = 45\n"
	if err := os.WriteFile(filepath.Join(dir, ".iris.toml"), []byte(toml), 0o644); err != nil {
		t.Fatalf("write .iris.toml: %v", err)
	}
	if got := gitTransferTimeout(dir); got != 45*time.Second {
		t.Fatalf("gitTransferTimeout(configured) = %s, want %s", got, 45*time.Second)
	}
}

func TestGitTransferTimeout_DefaultsOnMalformedConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".iris.toml"), []byte("this is not valid toml {{{"), 0o644); err != nil {
		t.Fatalf("write .iris.toml: %v", err)
	}
	if got := gitTransferTimeout(dir); got != 300*time.Second {
		t.Fatalf("gitTransferTimeout(malformed config) = %s, want default %s", got, 300*time.Second)
	}
}

// --- runGitTransfer ----------------------------------------------------

// TestRunGitTransfer_DecoupledFromCallerContext is the core regression test
// for the fix: cancelling the ctx passed into runGitTransfer must NOT kill
// an in-flight git push, because the transfer runs under its own context
// derived from context.WithoutCancel(ctx), not ctx itself.
func TestRunGitTransfer_DecoupledFromCallerContext(t *testing.T) {
	t.Parallel()
	_, wt, bare := setupRepoWithBareAndWorktree(t, "transfer-decouple")
	marker := filepath.Join(t.TempDir(), "prereceive-started")
	setSlowPreReceiveHook(t, bare, marker, 300*time.Millisecond)

	g := gitRunner(t)
	g(wt, "checkout", "-b", "feature-decouple")

	ctx, cancel := context.WithCancel(context.Background())

	type outcome struct {
		out string
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		out, err := runGitTransfer(ctx, wt, 5*time.Second, "push", "origin", "feature-decouple")
		done <- outcome{out, err}
	}()

	waitForMarker(t, marker, 2*time.Second)
	cancel() // simulate the inbound request context dying mid-transfer

	select {
	case o := <-done:
		if o.err != nil {
			t.Fatalf("runGitTransfer returned error after caller ctx cancellation: %v (out=%s)", o.err, o.out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runGitTransfer did not return in time")
	}

	if remoteRef(t, bare, "feature-decouple") == "" {
		t.Fatal("expected feature-decouple to have been pushed to bare origin despite caller ctx cancellation")
	}
}

// TestRunGitTransfer_TimesOutWithGitTransferError proves the timeout is
// iris's own, configurable deadline: a timeout shorter than the hook's
// sleep fires at approximately the configured duration.
func TestRunGitTransfer_TimesOutWithGitTransferError(t *testing.T) {
	t.Parallel()
	_, wt, bare := setupRepoWithBareAndWorktree(t, "transfer-timeout")
	marker := filepath.Join(t.TempDir(), "prereceive-started")
	setSlowPreReceiveHook(t, bare, marker, 3*time.Second)

	g := gitRunner(t)
	g(wt, "checkout", "-b", "feature-timeout")

	const configured = 500 * time.Millisecond
	start := time.Now()
	_, err := runGitTransfer(context.Background(), wt, configured, "push", "origin", "feature-timeout")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !IsGitTransferTimeout(err) {
		t.Fatalf("expected IsGitTransferTimeout(err) = true, got err: %v", err)
	}
	if elapsed < configured {
		t.Fatalf("runGitTransfer returned in %s, before the configured timeout %s elapsed", elapsed, configured)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("runGitTransfer took %s, expected it to respect the configured %s timeout (not the hook's 3s sleep)", elapsed, configured)
	}
}
