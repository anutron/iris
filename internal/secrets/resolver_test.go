package secrets

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anutron/iris/internal/config"
)

// captureLogs redirects slog's default logger to an in-memory buffer for the
// duration of the test, restoring the previous default on cleanup. Returns
// the buffer so the test can inspect exactly what was logged.
func captureLogs(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// swapSubprocessRunner installs fn as the secretSubprocessRunner seam for
// the duration of the test, restoring the original on cleanup. Every test
// that would otherwise shell out to a real `security`/`op` binary must use
// this instead.
func swapSubprocessRunner(t *testing.T, fn func(ctx context.Context, name string, args []string, extraEnv []string, timeout time.Duration) (string, bool)) {
	t.Helper()
	old := secretSubprocessRunner
	secretSubprocessRunner = fn
	t.Cleanup(func() { secretSubprocessRunner = old })
}

// resetMemo clears the package-level memoization cache before and after the
// test so cross-test descriptor reuse (e.g. two tests both using
// "env://FOO") never leaks cached state between them.
func resetMemo(t *testing.T) {
	t.Helper()
	ResetMemoCache()
	t.Cleanup(ResetMemoCache)
}

// --- splitSecretScheme --------------------------------------------------

func TestSplitSecretScheme(t *testing.T) {
	cases := []struct {
		name       string
		source     string
		wantScheme string
		wantRest   string
	}{
		{"bare string defaults to env", "FOO_VAR", "env", "FOO_VAR"},
		{"explicit env scheme", "env://FOO_VAR", "env", "FOO_VAR"},
		{"keychain no account", "keychain://my-service", "keychain", "my-service"},
		{"keychain with account", "keychain://my-service/my-account", "keychain", "my-service/my-account"},
		{"op descriptor", "op://vault/item/field", "op", "vault/item/field"},
		{"unrecognized scheme", "vault://x", "vault", "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scheme, rest := splitSecretScheme(tc.source)
			if scheme != tc.wantScheme || rest != tc.wantRest {
				t.Fatalf("splitSecretScheme(%q) = (%q, %q), want (%q, %q)",
					tc.source, scheme, rest, tc.wantScheme, tc.wantRest)
			}
		})
	}
}

// --- env scheme -----------------------------------------------------------

func TestResolve_BareStringResolvesAgainstProcessEnv(t *testing.T) {
	resetMemo(t)
	t.Setenv("IRIS_SECRETS_TEST_BARE", "bare-value")

	got, ok := resolve(context.Background(), config.SecretsBlock{}, "IRIS_SECRETS_TEST_BARE")
	if !ok || got != "bare-value" {
		t.Fatalf("resolve(bare string) = (%q, %v), want (\"bare-value\", true)", got, ok)
	}
}

func TestResolve_EnvSchemeResolvesAgainstProcessEnv(t *testing.T) {
	resetMemo(t)
	t.Setenv("IRIS_SECRETS_TEST_ENV", "env-value")

	got, ok := resolve(context.Background(), config.SecretsBlock{}, "env://IRIS_SECRETS_TEST_ENV")
	if !ok || got != "env-value" {
		t.Fatalf("resolve(env://) = (%q, %v), want (\"env-value\", true)", got, ok)
	}
}

func TestResolve_EnvSchemeUnsetVarFails(t *testing.T) {
	resetMemo(t)
	os.Unsetenv("IRIS_SECRETS_TEST_UNSET")

	got, ok := resolve(context.Background(), config.SecretsBlock{}, "env://IRIS_SECRETS_TEST_UNSET")
	if ok {
		t.Fatalf("resolve(unset env://) = (%q, true), want ok=false", got)
	}
}

// --- keychain scheme --------------------------------------------------------

func TestKeychainSchemeResolve_NoAccount(t *testing.T) {
	resetMemo(t)
	var gotName string
	var gotArgs []string
	swapSubprocessRunner(t, func(_ context.Context, name string, args []string, extraEnv []string, _ time.Duration) (string, bool) {
		gotName = name
		gotArgs = append([]string(nil), args...)
		if len(extraEnv) != 0 {
			t.Fatalf("keychain resolver must not pass extraEnv, got %v", extraEnv)
		}
		return "keychain-secret", true
	})

	got, ok := resolve(context.Background(), config.SecretsBlock{}, "keychain://my-service")
	if !ok || got != "keychain-secret" {
		t.Fatalf("resolve(keychain://) = (%q, %v), want (\"keychain-secret\", true)", got, ok)
	}
	if gotName != "security" {
		t.Fatalf("subprocess name = %q, want \"security\"", gotName)
	}
	want := []string{"find-generic-password", "-s", "my-service", "-w"}
	if !slices.Equal(gotArgs, want) {
		t.Fatalf("subprocess args = %v, want %v", gotArgs, want)
	}
}

func TestKeychainSchemeResolve_WithAccount(t *testing.T) {
	resetMemo(t)
	var gotArgs []string
	swapSubprocessRunner(t, func(_ context.Context, _ string, args []string, _ []string, _ time.Duration) (string, bool) {
		gotArgs = append([]string(nil), args...)
		return "keychain-secret", true
	})

	got, ok := resolve(context.Background(), config.SecretsBlock{}, "keychain://my-service/my-account")
	if !ok || got != "keychain-secret" {
		t.Fatalf("resolve(keychain:// with account) = (%q, %v), want (\"keychain-secret\", true)", got, ok)
	}
	want := []string{"find-generic-password", "-s", "my-service", "-a", "my-account", "-w"}
	if !slices.Equal(gotArgs, want) {
		t.Fatalf("subprocess args = %v, want %v", gotArgs, want)
	}
}

func TestKeychainSchemeResolve_EmptyStdoutFails(t *testing.T) {
	resetMemo(t)
	swapSubprocessRunner(t, func(_ context.Context, _ string, _ []string, _ []string, _ time.Duration) (string, bool) {
		return "", true // zero exit, but empty output — still a miss.
	})

	got, ok := resolve(context.Background(), config.SecretsBlock{}, "keychain://my-service")
	if ok {
		t.Fatalf("resolve(keychain:// empty stdout) = (%q, true), want ok=false", got)
	}
}

// --- op scheme --------------------------------------------------------------

type recordedCall struct {
	name     string
	args     []string
	extraEnv []string
}

func TestOpSchemeResolve_BootstrapHandoffViaEnv(t *testing.T) {
	resetMemo(t)
	t.Setenv("IRIS_SECRETS_TEST_OP_BOOTSTRAP", "bootstrap-cred")

	var calls []recordedCall
	swapSubprocessRunner(t, func(_ context.Context, name string, args []string, extraEnv []string, _ time.Duration) (string, bool) {
		calls = append(calls, recordedCall{name, append([]string(nil), args...), append([]string(nil), extraEnv...)})
		if name == "op" {
			return "op-secret-value", true
		}
		return "", false
	})

	sc := config.SecretsBlock{
		Op: config.OpSecretConfig{
			BootstrapSource: "env://IRIS_SECRETS_TEST_OP_BOOTSTRAP",
			BootstrapTarget: "OP_SERVICE_ACCOUNT_TOKEN",
		},
	}

	got, ok := resolve(context.Background(), sc, "op://vault/item/field")
	if !ok || got != "op-secret-value" {
		t.Fatalf("resolve(op://) = (%q, %v), want (\"op-secret-value\", true)", got, ok)
	}

	// env:// bootstrap resolves via os.LookupEnv directly (no subprocess),
	// so exactly one subprocess call is expected: the `op read` itself.
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 subprocess call (op only), got %d: %+v", len(calls), calls)
	}
	if calls[0].name != "op" {
		t.Fatalf("subprocess name = %q, want \"op\"", calls[0].name)
	}
	wantArgs := []string{"read", "op://vault/item/field"}
	if !slices.Equal(calls[0].args, wantArgs) {
		t.Fatalf("op args = %v, want %v", calls[0].args, wantArgs)
	}
	wantEnv := []string{"OP_SERVICE_ACCOUNT_TOKEN=bootstrap-cred"}
	if !slices.Equal(calls[0].extraEnv, wantEnv) {
		t.Fatalf("op extraEnv = %v, want %v", calls[0].extraEnv, wantEnv)
	}
}

func TestOpSchemeResolve_BootstrapHandoffViaKeychain(t *testing.T) {
	resetMemo(t)
	var calls []recordedCall
	swapSubprocessRunner(t, func(_ context.Context, name string, args []string, extraEnv []string, _ time.Duration) (string, bool) {
		calls = append(calls, recordedCall{name, append([]string(nil), args...), append([]string(nil), extraEnv...)})
		switch name {
		case "security":
			return "bootstrap-from-keychain", true
		case "op":
			return "op-secret-value", true
		}
		return "", false
	})

	sc := config.SecretsBlock{
		Op: config.OpSecretConfig{
			BootstrapSource: "keychain://op-service-account",
			BootstrapTarget: "OP_SERVICE_ACCOUNT_TOKEN",
		},
	}

	got, ok := resolve(context.Background(), sc, "op://vault/item/field")
	if !ok || got != "op-secret-value" {
		t.Fatalf("resolve(op:// via keychain bootstrap) = (%q, %v), want (\"op-secret-value\", true)", got, ok)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 subprocess calls (security then op), got %d: %+v", len(calls), calls)
	}
	if calls[0].name != "security" {
		t.Fatalf("first call = %q, want \"security\" (bootstrap resolves first)", calls[0].name)
	}
	if calls[1].name != "op" {
		t.Fatalf("second call = %q, want \"op\"", calls[1].name)
	}
	wantEnv := []string{"OP_SERVICE_ACCOUNT_TOKEN=bootstrap-from-keychain"}
	if !slices.Equal(calls[1].extraEnv, wantEnv) {
		t.Fatalf("op extraEnv = %v, want %v", calls[1].extraEnv, wantEnv)
	}
}

func TestOpSchemeResolve_UnconfiguredBootstrapShortCircuits(t *testing.T) {
	resetMemo(t)
	called := false
	swapSubprocessRunner(t, func(_ context.Context, _ string, _ []string, _ []string, _ time.Duration) (string, bool) {
		called = true
		return "should-never-be-returned", true
	})

	sc := config.SecretsBlock{} // Op.BootstrapSource is "" — unconfigured.
	got, ok := resolve(context.Background(), sc, "op://vault/item/field")
	if ok {
		t.Fatalf("resolve(op:// unconfigured bootstrap) = (%q, true), want ok=false", got)
	}
	if called {
		t.Fatalf("op:// must not invoke any subprocess when bootstrap_source is unconfigured")
	}
}

func TestOpSchemeResolve_FailedBootstrapShortCircuits(t *testing.T) {
	resetMemo(t)
	var calls []recordedCall
	swapSubprocessRunner(t, func(_ context.Context, name string, args []string, extraEnv []string, _ time.Duration) (string, bool) {
		calls = append(calls, recordedCall{name, args, extraEnv})
		return "", false // bootstrap (security) fails.
	})

	sc := config.SecretsBlock{
		Op: config.OpSecretConfig{
			BootstrapSource: "keychain://bad-service",
			BootstrapTarget: "OP_SERVICE_ACCOUNT_TOKEN",
		},
	}
	got, ok := resolve(context.Background(), sc, "op://vault/item/field")
	if ok {
		t.Fatalf("resolve(op:// failed bootstrap) = (%q, true), want ok=false", got)
	}
	if len(calls) != 1 || calls[0].name != "security" {
		t.Fatalf("expected exactly 1 subprocess call (security only, op never invoked), got %d: %+v", len(calls), calls)
	}
}

// --- unrecognized scheme -----------------------------------------------------

func TestResolve_UnrecognizedSchemeFailsCleanly(t *testing.T) {
	resetMemo(t)
	called := false
	swapSubprocessRunner(t, func(_ context.Context, _ string, _ []string, _ []string, _ time.Duration) (string, bool) {
		called = true
		return "x", true
	})

	got, ok := resolve(context.Background(), config.SecretsBlock{}, "vault://x")
	if ok {
		t.Fatalf("resolve(vault://x) = (%q, true), want ok=false", got)
	}
	if called {
		t.Fatalf("unrecognized scheme must never invoke a subprocess")
	}
}

// --- memoization --------------------------------------------------------

func TestResolve_MemoizesSuccessfulResolve(t *testing.T) {
	resetMemo(t)
	calls := 0
	swapSubprocessRunner(t, func(_ context.Context, _ string, _ []string, _ []string, _ time.Duration) (string, bool) {
		calls++
		return "cached-value", true
	})

	sc := config.SecretsBlock{}
	first, ok := resolve(context.Background(), sc, "keychain://svc")
	if !ok || first != "cached-value" {
		t.Fatalf("first resolve = (%q, %v)", first, ok)
	}
	second, ok := resolve(context.Background(), sc, "keychain://svc")
	if !ok || second != "cached-value" {
		t.Fatalf("second resolve = (%q, %v)", second, ok)
	}
	if calls != 1 {
		t.Fatalf("subprocess invoked %d times, want exactly 1 (second resolve should hit the cache)", calls)
	}
}

func TestResolve_DoesNotMemoizeFailedResolve(t *testing.T) {
	resetMemo(t)
	calls := 0
	swapSubprocessRunner(t, func(_ context.Context, _ string, _ []string, _ []string, _ time.Duration) (string, bool) {
		calls++
		return "", false
	})

	sc := config.SecretsBlock{}
	if _, ok := resolve(context.Background(), sc, "keychain://svc"); ok {
		t.Fatalf("expected first resolve to fail")
	}
	if _, ok := resolve(context.Background(), sc, "keychain://svc"); ok {
		t.Fatalf("expected second resolve to fail")
	}
	if calls != 2 {
		t.Fatalf("subprocess invoked %d times, want exactly 2 (a failed resolve must never be cached)", calls)
	}
}

func TestResolve_FailureThenSuccessNotPoisonedByCache(t *testing.T) {
	resetMemo(t)
	calls := 0
	swapSubprocessRunner(t, func(_ context.Context, _ string, _ []string, _ []string, _ time.Duration) (string, bool) {
		calls++
		if calls == 1 {
			return "", false // transient failure.
		}
		return "recovered-value", true
	})

	sc := config.SecretsBlock{}
	if _, ok := resolve(context.Background(), sc, "keychain://svc"); ok {
		t.Fatalf("expected first (transient) resolve to fail")
	}
	got, ok := resolve(context.Background(), sc, "keychain://svc")
	if !ok || got != "recovered-value" {
		t.Fatalf("second resolve = (%q, %v), want (\"recovered-value\", true)", got, ok)
	}
}

// --- commandResolvable (exec.LookPath, not os.Stat) --------------------------

func TestCommandResolvable_RejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	if commandResolvable(dir) {
		t.Fatalf("commandResolvable(%q) = true, want false for a directory", dir)
	}
}

func TestCommandResolvable_RejectsNonExecutableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-executable")
	if err := os.WriteFile(path, []byte("not a script"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if commandResolvable(path) {
		t.Fatalf("commandResolvable(%q) = true, want false for a non-executable file", path)
	}
}

func TestCommandResolvable_AcceptsExecutableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "executable")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if !commandResolvable(path) {
		t.Fatalf("commandResolvable(%q) = false, want true for an executable file", path)
	}
}

// --- subprocess-safety: process-group kill on timeout ------------------------

// writeForkingFixture writes an executable shell script that forks a
// background descendant holding stdout open and sleeping far longer than
// any timeout used below, while the script itself (the direct child
// exec.Cmd manages) also blocks past the timeout. This reproduces the
// "child forks a descendant that inherits the output pipe" shape that a
// direct-child-only kill fails to bound (see design.md's PR #928 citation).
func writeForkingFixture(t *testing.T, sleepSeconds int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.sh")
	body := "#!/bin/sh\n" +
		"(sleep " + itoa(sleepSeconds) + ") &\n" +
		"sleep " + itoa(sleepSeconds) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fixture script: %v", err)
	}
	return path
}

func itoa(n int) string {
	// Avoid importing strconv just for this; n is always a small positive
	// literal in this file's callers.
	digits := "0123456789"
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{digits[n%10]}, out...)
		n /= 10
	}
	return string(out)
}

func TestDefaultSecretSubprocessRunner_KillsWholeProcessGroupOnTimeout(t *testing.T) {
	fixture := writeForkingFixture(t, 8) // descendant sleeps far past the timeout below.

	start := time.Now()
	stdout, ok := defaultSecretSubprocessRunner(context.Background(), fixture, nil, nil, 150*time.Millisecond)
	elapsed := time.Since(start)

	if ok {
		t.Fatalf("expected the timed-out subprocess to be treated as a failed resolve, got ok=true stdout=%q", stdout)
	}
	// If only the direct child were killed (leaving the backgrounded
	// descendant holding stdout open), Wait() would block for the
	// descendant's full sleep duration. Bounded well under that proves the
	// whole process group was killed, not just cmd.Process.
	if elapsed > 4*time.Second {
		t.Fatalf("subprocess not bounded by timeout+process-group-kill: took %v (fixture sleeps 8s)", elapsed)
	}
}

func TestDefaultSecretSubprocessRunner_UnresolvableCommandFailsWithoutRunning(t *testing.T) {
	dir := t.TempDir() // a directory is never resolvable.
	stdout, ok := defaultSecretSubprocessRunner(context.Background(), dir, nil, nil, time.Second)
	if ok {
		t.Fatalf("expected unresolvable command to fail, got ok=true stdout=%q", stdout)
	}
}

// --- ResolveEnv ------------------------------------------------------------

func TestResolveEnv_InjectsEveryResolvableMapping(t *testing.T) {
	resetMemo(t)
	t.Setenv("IRIS_SECRETS_TEST_RESOLVEENV", "resolveenv-value")

	sc := config.SecretsBlock{
		Env: map[string]string{
			"TARGET_ONE": "env://IRIS_SECRETS_TEST_RESOLVEENV",
			"TARGET_TWO": "IRIS_SECRETS_TEST_RESOLVEENV", // bare string, same var.
		},
	}
	got := ResolveEnv(context.Background(), sc)
	want := map[string]bool{
		"TARGET_ONE=resolveenv-value": false,
		"TARGET_TWO=resolveenv-value": false,
	}
	if len(got) != len(want) {
		t.Fatalf("ResolveEnv returned %d entries, want %d: %v", len(got), len(want), got)
	}
	for _, entry := range got {
		if _, ok := want[entry]; !ok {
			t.Fatalf("unexpected entry %q in %v", entry, got)
		}
		want[entry] = true
	}
	for entry, seen := range want {
		if !seen {
			t.Fatalf("missing expected entry %q in %v", entry, got)
		}
	}
}

func TestResolveEnv_AbsentSecretsBlockIsNoop(t *testing.T) {
	resetMemo(t)
	got := ResolveEnv(context.Background(), config.SecretsBlock{})
	if len(got) != 0 {
		t.Fatalf("ResolveEnv(zero SecretsBlock) = %v, want empty", got)
	}
}

func TestResolveEnv_UnresolvedSourceLeavesTargetUnsetAndWarns(t *testing.T) {
	resetMemo(t)
	os.Unsetenv("IRIS_SECRETS_TEST_MISSING")
	buf := captureLogs(t)

	sc := config.SecretsBlock{
		Env: map[string]string{
			"SHOULD_STAY_UNSET": "env://IRIS_SECRETS_TEST_MISSING",
		},
	}
	got := ResolveEnv(context.Background(), sc)
	if len(got) != 0 {
		t.Fatalf("ResolveEnv = %v, want no entries for an unresolved source", got)
	}
	logged := buf.String()
	if !strings.Contains(logged, "SHOULD_STAY_UNSET") {
		t.Fatalf("expected warning naming the target variable, got log: %s", logged)
	}
	if !strings.Contains(logged, "env://IRIS_SECRETS_TEST_MISSING") {
		t.Fatalf("expected warning naming the source descriptor, got log: %s", logged)
	}
}

func TestResolveEnv_UnrecognizedSchemeIsSkippedNotFatal(t *testing.T) {
	resetMemo(t)
	sc := config.SecretsBlock{
		Env: map[string]string{"TARGET": "vault://x"},
	}
	got := ResolveEnv(context.Background(), sc)
	if len(got) != 0 {
		t.Fatalf("ResolveEnv = %v, want no entries for an unrecognized scheme", got)
	}
}

// TestResolveEnv_NeverLogsResolvedValue is the security invariant test: a
// resolved secret value must never appear in anything ResolveEnv logs, for
// either a successful or a failed resolve alongside it.
func TestResolveEnv_NeverLogsResolvedValue(t *testing.T) {
	resetMemo(t)
	const secretValue = "super-secret-value-should-never-be-logged-4f8c2a"
	t.Setenv("IRIS_SECRETS_TEST_SECRETVAL", secretValue)
	os.Unsetenv("IRIS_SECRETS_TEST_MISSING_FOR_LOG")
	buf := captureLogs(t)

	sc := config.SecretsBlock{
		Env: map[string]string{
			"RESOLVED_OK": "env://IRIS_SECRETS_TEST_SECRETVAL",
			"UNRESOLVED":  "env://IRIS_SECRETS_TEST_MISSING_FOR_LOG",
		},
	}
	got := ResolveEnv(context.Background(), sc)

	foundResolved := false
	for _, entry := range got {
		if entry == "RESOLVED_OK="+secretValue {
			foundResolved = true
		}
	}
	if !foundResolved {
		t.Fatalf("expected RESOLVED_OK=%s in ResolveEnv output, got %v", secretValue, got)
	}

	logged := buf.String()
	if strings.Contains(logged, secretValue) {
		t.Fatalf("resolved secret value leaked into log output: %s", logged)
	}
	// Sanity: the failure path itself did produce a log line (naming only
	// the variable/descriptor), so this test isn't vacuously passing
	// because nothing was logged at all.
	if !strings.Contains(logged, "UNRESOLVED") {
		t.Fatalf("expected the failure to be logged (naming the variable), got: %s", logged)
	}
}

func TestResolveEnv_KeychainAndOpSourcesNeverInvokeRealBinaries(t *testing.T) {
	resetMemo(t)
	swapSubprocessRunner(t, func(_ context.Context, name string, _ []string, _ []string, _ time.Duration) (string, bool) {
		switch name {
		case "security":
			return "keychain-fake-value", true
		case "op":
			return "op-fake-value", true
		}
		t.Fatalf("unexpected subprocess name %q", name)
		return "", false
	})

	sc := config.SecretsBlock{
		Env: map[string]string{
			"FROM_KEYCHAIN": "keychain://svc",
			"FROM_OP":       "op://vault/item/field",
		},
		Op: config.OpSecretConfig{
			BootstrapSource: "keychain://bootstrap-svc",
			BootstrapTarget: "OP_SERVICE_ACCOUNT_TOKEN",
		},
	}
	got := ResolveEnv(context.Background(), sc)
	wantSet := map[string]bool{
		"FROM_KEYCHAIN=keychain-fake-value": false,
		"FROM_OP=op-fake-value":             false,
	}
	for _, entry := range got {
		if _, ok := wantSet[entry]; !ok {
			t.Fatalf("unexpected entry %q in %v", entry, got)
		}
		wantSet[entry] = true
	}
	for entry, seen := range wantSet {
		if !seen {
			t.Fatalf("missing expected entry %q in %v", entry, got)
		}
	}
}
