package verbs

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/anutron/iris/internal/config"
)

// gitTransferWaitDelay bounds how long runGitTransfer's Wait() call spends
// waiting on I/O after the transfer's context is done or the git process
// exits, whichever occurs first (see os/exec's Cmd.WaitDelay). Without
// this, a killed git push/fetch process can leave an orphaned grandchild it
// spawned (e.g. a server-side receive-pack/upload-pack process still mid a
// slow pre-receive hook) holding the shared stdout/stderr pipe open, and
// Wait() would block reading that pipe until the grandchild eventually
// exits on its own — potentially far past the configured timeout, and
// exactly the kind of hang this change exists to prevent. The delay is
// short because it only matters on the abnormal path: in ordinary
// completion, the git process's own I/O closes immediately, well inside
// this window.
const gitTransferWaitDelay = 1 * time.Second

// postTransferReadTimeout bounds the small, local git read that immediately
// follows a successful push/fetch transfer to build the verb's result
// (push's rev-parse of the resulting remote SHA; fetch's post-fetch ref
// snapshot). This read runs detached from the caller's ctx the same way
// the transfer itself does — the mutation has already succeeded by this
// point, so a caller's context dying in that same instant must not turn a
// genuine success into a reported failure. It is a fixed constant, not the
// configurable git_transfer_timeout_seconds, because it is a fast, purely
// local operation regardless of network conditions.
const postTransferReadTimeout = 10 * time.Second

// GitTransferReason classifies why a git push/fetch network operation
// failed, so a caller can tell iris's own deadline apart from a git-level
// auth/network/other failure without parsing free-text output.
type GitTransferReason string

const (
	// GitTransferTimeout means iris's own configured deadline fired before
	// the operation completed — not a network or auth failure. Whether the
	// operation nonetheless completed server-side before iris's local
	// process was killed is unknown; callers should check state (e.g.
	// iris:fetch, iris:status) rather than assume either outcome.
	GitTransferTimeout GitTransferReason = "timeout"

	// GitTransferAuthFailure means git's output matched a known
	// credential/permission failure pattern. A mechanical retry is
	// unlikely to help without fixing auth.
	GitTransferAuthFailure GitTransferReason = "auth_failure"

	// GitTransferNetworkFailure means git's output matched a known
	// connectivity failure pattern. This may be transient; a retry could
	// help.
	GitTransferNetworkFailure GitTransferReason = "network_failure"

	// GitTransferOtherFailure covers any other non-zero git exit,
	// including non-fast-forward and unrecognized failures. This is the
	// conservative default: an unclassified failure is never mislabeled as
	// auth or network.
	GitTransferOtherFailure GitTransferReason = "other_failure"
)

// GitTransferError wraps a classified git push/fetch failure. Op is
// "push" or "fetch" (the git subcommand). Timeout is populated only when
// Reason is GitTransferTimeout.
type GitTransferError struct {
	Op      string
	Reason  GitTransferReason
	Timeout time.Duration
	Err     error
}

// Error implements the error interface with reason-specific, actionable
// text so the classification is legible even when only the error's string
// form survives (MCP tool responses are text-only).
func (e *GitTransferError) Error() string {
	switch e.Reason {
	case GitTransferTimeout:
		return fmt.Sprintf(
			"git %s: [timeout] iris's own git_transfer_timeout_seconds (%s) elapsed before the operation finished — this is iris's own deadline, not a network or auth failure; consider raising git_transfer_timeout_seconds in .iris.toml, or check state (iris:fetch / iris:status) before retrying: %v",
			e.Op, e.Timeout, e.Err,
		)
	case GitTransferAuthFailure:
		return fmt.Sprintf("git %s: [auth_failure] looks like a credential/permission problem, not a timeout — a retry is unlikely to help without fixing auth: %v", e.Op, e.Err)
	case GitTransferNetworkFailure:
		return fmt.Sprintf("git %s: [network_failure] looks like a network/connectivity problem — may be transient, a retry could help: %v", e.Op, e.Err)
	default:
		return fmt.Sprintf("git %s: [other_failure] %v", e.Op, e.Err)
	}
}

// Unwrap supports errors.Is/As against the underlying error.
func (e *GitTransferError) Unwrap() error { return e.Err }

// IsGitTransferTimeout reports whether err is (or wraps) a *GitTransferError
// whose Reason is GitTransferTimeout — i.e. iris's own deadline fired, as
// opposed to a network/auth/other git failure.
func IsGitTransferTimeout(err error) bool {
	var gtErr *GitTransferError
	return errors.As(err, &gtErr) && gtErr.Reason == GitTransferTimeout
}

// gitTransferAuthPatterns and gitTransferNetworkPatterns are known,
// lowercased substrings of git's combined stdout+stderr output that
// reliably indicate an auth or network failure respectively. Deliberately
// conservative: an unmatched failure falls through to GitTransferOtherFailure
// rather than being guessed at.
var (
	gitTransferAuthPatterns = []string{
		"permission denied",
		"authentication failed",
		"could not read username",
		"could not read password",
		"invalid username or password",
		"403",
		"access denied",
	}
	gitTransferNetworkPatterns = []string{
		"could not resolve host",
		"could not resolve hostname",
		"connection refused",
		"connection timed out",
		"network is unreachable",
		"could not connect to server",
		"couldn't connect to server",
		"connect to host",
		"unable to access",
	}
)

// classifyGitFailure inspects git's combined output and returns the best
// matching GitTransferReason. Auth patterns are checked before network
// patterns since some auth failures are surfaced over the network-lookup
// vocabulary (e.g. "unable to access ... 403"); an unrecognized output
// classifies as GitTransferOtherFailure.
func classifyGitFailure(output string) GitTransferReason {
	lower := strings.ToLower(output)
	for _, p := range gitTransferAuthPatterns {
		if strings.Contains(lower, p) {
			return GitTransferAuthFailure
		}
	}
	for _, p := range gitTransferNetworkPatterns {
		if strings.Contains(lower, p) {
			return GitTransferNetworkFailure
		}
	}
	return GitTransferOtherFailure
}

// gitTransferTimeout loads the configured git_transfer_timeout_seconds from
// .iris.toml in sourceRepo, defaulting when the file is absent, unreadable,
// malformed, or the field is unset. A broken .iris.toml is not fatal here —
// that's a validate_config/build-hook concern, not a reason to refuse a
// push/fetch.
func gitTransferTimeout(sourceRepo string) time.Duration {
	configPath := filepath.Join(sourceRepo, config.IrisTomlFilename)
	doc, _, err := config.LoadIrisToml(configPath, false)
	if err != nil || doc == nil {
		return time.Duration(config.DefaultGitTransferTimeoutSeconds) * time.Second
	}
	return time.Duration(doc.ResolvedGitTransferTimeoutSeconds()) * time.Second
}

// runGitTransfer runs a git network operation (push/fetch) under a timeout
// owned by iris, decoupled from the caller-supplied ctx's cancellation and
// deadline via context.WithoutCancel: an inbound MCP request context dying
// (e.g. because argus's client gave up waiting) does not kill an
// otherwise-healthy in-flight transfer. Any values on ctx remain available
// to the detached context.
//
// On failure, the error is a *GitTransferError classifying the failure as
// GitTransferTimeout (iris's own deadline fired — detected via the
// transfer's own context, not the process's exit error, per the os/exec
// docs' recommended pattern) or, via classifyGitFailure, an auth/network/
// other failure.
func runGitTransfer(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error) {
	transferCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(transferCtx, "git", full...)
	cmd.WaitDelay = gitTransferWaitDelay
	rawOut, runErr := cmd.CombinedOutput()
	out := string(rawOut)
	if runErr == nil {
		return out, nil
	}

	op := ""
	if len(args) > 0 {
		op = args[0]
	}
	wrapped := fmt.Errorf("git %s: %w: %s", strings.Join(full, " "), runErr, strings.TrimSpace(out))

	if transferCtx.Err() == context.DeadlineExceeded {
		return out, &GitTransferError{Op: op, Reason: GitTransferTimeout, Timeout: timeout, Err: wrapped}
	}
	return out, &GitTransferError{Op: op, Reason: classifyGitFailure(out), Err: wrapped}
}
