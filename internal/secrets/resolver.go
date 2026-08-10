// Package secrets implements iris's secret-source resolver registry: a
// scheme-prefixed descriptor ("env://", "keychain://", "op://", or a bare
// string defaulting to env://) is dispatched to a scheme-specific resolver,
// with process-lifetime success-only memoization in front of the whole
// thing. A secret is resolved fresh, at the point of use, and injected only
// into the one subprocess's own environment it was resolved for — never via
// os.Setenv on iris's own process.
//
// See openspec/changes/add-secrets-resolver/design.md for the full
// rationale. This package is a deliberate, adapted port of argus's own
// internal/agent/secretregistry.go (a sibling project that solved the exact
// same "resolve fresh, at the point of use" problem first): the
// scheme-dispatch shape, the memoization shape, and the subprocess-safety
// fix (exec.LookPath over os.Stat; Setpgid + a Cancel that kills the whole
// process group + a WaitDelay backstop) are ported as-is, adapted to this
// codebase's own conventions — log/slog instead of a custom logger, and ctx
// threaded all the way down to the resolver subprocess instead of
// hardcoding context.Background(), so a cancelled build/check/restart also
// cancels an in-flight resolver subprocess.
package secrets

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anutron/iris/internal/config"
)

// keychainCommandTimeout bounds a `security find-generic-password`
// subprocess call. Keychain lookups are local and should return
// near-instantly; this is generous headroom, not an expected steady-state
// latency.
const keychainCommandTimeout = 5 * time.Second

// opCommandTimeout bounds an `op read` subprocess call. Unlike the keychain
// lookup, this may cross the network (op Connect/cloud), so it gets more
// headroom than keychainCommandTimeout.
const opCommandTimeout = 15 * time.Second

// subprocessWaitDelay backstops cmd.Wait() after a process-group kill on
// timeout. The kill itself (see defaultSecretSubprocessRunner) should close
// the subprocess's stdout pipe almost immediately; this only guards against
// a pipe that somehow stays open a moment longer.
const subprocessWaitDelay = 2 * time.Second

// splitSecretScheme splits a source descriptor on the first "://",
// defaulting the scheme to "env" when absent — a bare string is a
// first-class alias for env://<string>.
func splitSecretScheme(source string) (scheme, rest string) {
	if s, r, found := strings.Cut(source, "://"); found {
		return s, r
	}
	return "env", source
}

// --- process-lifetime success-only memoization ------------------------------

var (
	memoMu sync.Mutex
	memo   = map[string]string{}
)

// ResetMemoCache clears the process-lifetime resolve cache. Exported for
// tests only — production code never needs to clear it, since success-only
// memoization is meant to live for the process's remaining lifetime. Tests
// in OTHER packages that exercise ResolveEnv repeatedly with overlapping
// descriptor strings across test functions (e.g. internal/verbs wiring
// tests) should call this between tests to avoid cross-test contamination
// from this package-level cache.
func ResetMemoCache() {
	memoMu.Lock()
	defer memoMu.Unlock()
	memo = map[string]string{}
}

// resolve resolves a single secret source descriptor through the
// scheme-prefixed registry (see splitSecretScheme), memoizing a SUCCESSFUL
// resolve for the remaining lifetime of the process, keyed by the exact
// descriptor string passed in. A failed resolve is never cached, so a
// transient failure (e.g. a network blip on `op read`) can succeed on a
// later attempt. sc supplies any scheme-specific configuration a resolver
// needs (currently only the op scheme's [secrets.op] bootstrap config,
// consulted recursively — see opSchemeResolve). An unrecognized scheme is a
// failed resolve, never an error.
func resolve(ctx context.Context, sc config.SecretsBlock, source string) (string, bool) {
	memoMu.Lock()
	if v, ok := memo[source]; ok {
		memoMu.Unlock()
		return v, true
	}
	memoMu.Unlock()

	scheme, rest := splitSecretScheme(source)
	var (
		v  string
		ok bool
	)
	switch scheme {
	case "env":
		v, ok = envSchemeResolve(rest)
	case "keychain":
		v, ok = keychainSchemeResolve(ctx, rest)
	case "op":
		v, ok = opSchemeResolve(ctx, sc, rest)
	default:
		return "", false
	}
	if !ok {
		return "", false
	}

	memoMu.Lock()
	memo[source] = v
	memoMu.Unlock()
	return v, true
}

// envSchemeResolve resolves an env:// (or bare-string) source against
// iris's own process environment.
func envSchemeResolve(name string) (string, bool) {
	return os.LookupEnv(name)
}

// keychainSchemeResolve resolves "keychain://<service>" or
// "keychain://<service>/<account>" by shelling out to `security
// find-generic-password`. A non-zero exit or empty stdout is a failed
// resolve. The subprocess invocation runs through the injectable
// secretSubprocessRunner seam so tests never shell out to a real `security`.
func keychainSchemeResolve(ctx context.Context, rest string) (string, bool) {
	service, account, hasAccount := strings.Cut(rest, "/")
	args := []string{"find-generic-password", "-s", service}
	if hasAccount {
		args = append(args, "-a", account)
	}
	args = append(args, "-w")

	stdout, exitedZero := secretSubprocessRunner(ctx, "security", args, nil, keychainCommandTimeout)
	if !exitedZero || stdout == "" {
		return "", false
	}
	return stdout, true
}

// opSchemeResolve resolves "op://<vault>/<item>/<field>" by first resolving
// [secrets.op].bootstrap_source through the SAME resolve function used for
// any other source descriptor — deliberately no special-cased "how does op
// authenticate" code path (bootstrap_source may itself be keychain://,
// env://, or any other scheme this registry supports). If the bootstrap
// resolve fails, or bootstrap_source is empty (i.e. [secrets.op] is
// unconfigured), the op:// resolve fails immediately and `op read` is never
// even attempted. On a successful bootstrap, the resolved credential is set
// under bootstrap_target ONLY in the `op read` subprocess's own environment
// (via secretSubprocessRunner's extraEnv param) — never via os.Setenv on
// iris's own process.
//
// config validation (internal/config's (*SecretsBlock).validate) already
// rejects a bootstrap_source that itself begins with "op://" — this
// function does not re-check that itself, but does not assume the invalid
// state is unreachable either; it simply calls resolve exactly as it would
// for any other scheme, matching the "no special case" design decision.
func opSchemeResolve(ctx context.Context, sc config.SecretsBlock, rest string) (string, bool) {
	if sc.Op.BootstrapSource == "" {
		return "", false
	}
	bootstrap, ok := resolve(ctx, sc, sc.Op.BootstrapSource)
	if !ok {
		return "", false
	}

	args := []string{"read", "op://" + rest}
	extraEnv := []string{sc.Op.BootstrapTarget + "=" + bootstrap}

	stdout, exitedZero := secretSubprocessRunner(ctx, "op", args, extraEnv, opCommandTimeout)
	if !exitedZero || stdout == "" {
		return "", false
	}
	return stdout, true
}

// ResolveEnv resolves every `[secrets.env]` mapping in sc, returning
// ready-to-append "TARGET=value" strings for every source that resolves.
// This is the ONE shared helper every wiring call site (RunBuild, RunChecks,
// runBuildBlock, dispatchRestart) appends onto its own cmd.Env — never four
// copies of the same resolve-and-log loop. ctx is threaded through to any
// resolver subprocess call, so a cancelled build/check/restart also cancels
// an in-flight resolver subprocess, rather than leaving it to run out its
// own independent timeout after the caller has already given up.
//
// A source that fails to resolve is logged via slog.Warn, naming ONLY the
// target variable and the source descriptor string — never the resolved
// value. A successful resolve is never logged at all. ResolveEnv never
// returns an error and never panics: an absent or partially-unresolvable
// [secrets] configuration is always a safe no-op for the caller's
// build/check/restart to proceed with.
func ResolveEnv(ctx context.Context, sc config.SecretsBlock) []string {
	var out []string
	for target, source := range sc.Env {
		v, ok := resolve(ctx, sc, source)
		if !ok {
			slog.Warn("secret failed to resolve; leaving target variable unset",
				"target", target,
				"source", source,
			)
			continue
		}
		out = append(out, target+"="+v)
	}
	return out
}

// --- injectable subprocess execution seam -----------------------------------

// secretSubprocessRunner is the test seam for executing a resolver
// subprocess (security, op). It returns the command's trimmed stdout and
// whether the command exited with code 0 — NOT whether the caller should
// treat the result as resolved (an empty stdout with a zero exit is still a
// failed keychain/op lookup; that distinction is made by the scheme
// resolver, not here). extraEnv, when non-empty, is appended to a copy of
// the ambient environment for that subprocess only (used by the op
// resolver's bootstrap-credential handoff; never via os.Setenv on iris's
// own process). Tests swap this var to a fake so no unit test ever invokes
// a real security/op binary; production code always uses
// defaultSecretSubprocessRunner.
var secretSubprocessRunner = defaultSecretSubprocessRunner

// commandResolvable reports whether name is an actually-resolvable,
// executable command — via exec.LookPath, NOT os.Stat. os.Stat wrongly
// accepts a directory or a non-executable regular file as a match;
// exec.LookPath correctly rejects both while still resolving a bare name
// against PATH or an absolute/relative path directly.
func commandResolvable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// defaultSecretSubprocessRunner is the real subprocess-execution
// implementation behind secretSubprocessRunner. It bounds the subprocess
// with BOTH a context timeout and a process-group kill on cancellation —
// exec.CommandContext's default Cancel only signals the direct child
// process and silently fails to bound cmd.Wait() if that child forks a
// descendant that inherits and holds open the stdout pipe (empirically
// proven necessary: a short configured timeout otherwise waits out the
// descendant's own, much longer, lifetime — see design.md).
func defaultSecretSubprocessRunner(ctx context.Context, name string, args []string, extraEnv []string, timeout time.Duration) (string, bool) {
	if !commandResolvable(name) {
		return "", false
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	// Put the subprocess (and anything it forks) in its own process group so
	// a timeout kill can reach descendants, not just the direct child.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = subprocessWaitDelay
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			// Match exec.CommandContext's own default Cancel contract: a
			// process that already exited is not itself an error.
			return os.ErrProcessDone
		}
		return err
	}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", false
	}
	return strings.TrimSpace(stdout.String()), true
}
