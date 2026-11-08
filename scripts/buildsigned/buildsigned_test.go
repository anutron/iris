// Package buildsigned tests scripts/build-signed.sh against fake `security` and
// `codesign` binaries on PATH. The real keychain and signing identities are
// never touched, and the real script never builds iris.
package buildsigned

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fakeSecurity = `#!/bin/sh
case "$1" in
find-identity)
  [ "$FAKE_IDENTITY" = "none" ] && exit 0
  echo '  1) ABCDEF0123 "Argus Code Signing"'
  ;;
show-keychain-info)
  [ "$FAKE_KEYCHAIN" = "locked" ] && { echo "User interaction is not allowed." >&2; exit 36; }
  exit 0
  ;;
esac
`

// FAKE_CODESIGN=fail exits 1; =alarm kills itself with SIGALRM (what perl's
// alarm does at the deadline, but instantly); =hang blocks until the real alarm.
const fakeCodesign = `#!/bin/sh
echo "$@" >> "$FAKE_LOG"
case "$FAKE_CODESIGN" in
hang) exec sleep 600 ;;
alarm) kill -ALRM $$ ;;
fail) exit 1 ;;
esac
for last; do :; done
echo SIGNED >> "$last"
`

const fakeBuild = "#!/bin/sh\necho NEW > \"$1\"\n"

type result struct {
	out, dest, log string
	err            error
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
}

// run executes the script in a sandbox dir. withCodesign=false leaves the fake
// codesign off PATH (Linux CI shape). PATH is only fakebin plus a symlink farm
// of the few real tools the script needs, so a real codesign can never leak in.
func run(t *testing.T, withCodesign bool, env ...string) result {
	t.Helper()
	dir := t.TempDir()
	fb := filepath.Join(dir, "fakebin")
	if err := os.MkdirAll(fb, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"sh", "perl", "grep", "head", "awk", "mkdir", "mv", "rm", "dirname", "sleep", "kill", "echo", "cat"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			continue
		}
		if err := os.Symlink(p, filepath.Join(fb, tool)); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(fb, "security"), fakeSecurity)
	if withCodesign {
		write(t, filepath.Join(fb, "codesign"), fakeCodesign)
	}
	build := filepath.Join(dir, "build.sh")
	write(t, build, fakeBuild)
	dest := filepath.Join(dir, "bin", "iris")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dest, "OLD\n")
	logp := filepath.Join(dir, "codesign.log")
	script, err := filepath.Abs("../build-signed.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append([]string{
		"PATH=" + fb,
		"TMPDIR=" + dir,
		"IRIS_BUILD_OUT=" + dest,
		"IRIS_BUILD_CMD=" + build,
		"IRIS_SIGN_TIMEOUT=60", // fake probes are instant; generous for loaded hosts
		"FAKE_LOG=" + logp,
	}, env...)
	out, runErr := cmd.CombinedOutput()
	t.Logf("script output: %q", out)
	if left, _ := filepath.Glob(dest + ".new.*"); len(left) != 0 {
		t.Errorf("temp file left behind: %v", left)
	}
	b, _ := os.ReadFile(dest)
	lg, _ := os.ReadFile(logp)
	return result{string(out), string(b), string(lg), runErr}
}

func mustFail(t *testing.T, r result, wantOut string) {
	t.Helper()
	if r.err == nil {
		t.Fatal("expected non-zero exit")
	}
	if r.dest != "OLD\n" {
		t.Fatalf("existing binary was modified: %q", r.dest)
	}
	if !strings.Contains(r.out, wantOut) {
		t.Fatalf("output missing %q: %q", wantOut, r.out)
	}
}

func TestSignsTempThenInstallsAtomically(t *testing.T) {
	start := time.Now()
	r := run(t, true)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
	if r.dest != "NEW\nSIGNED\n" {
		t.Fatalf("dest = %q, want built+signed", r.dest)
	}
	// codesign targets the temp file, never the live destination, with the
	// stable identifier and the resolved identity hash.
	for _, want := range []string{"iris.new.", "--identifier com.anutron.iris", "--sign ABCDEF0123"} {
		if !strings.Contains(r.log, want) {
			t.Errorf("codesign args %q missing %q", r.log, want)
		}
	}
	// Instant stubs must never wait out their 60s timeout.
	if time.Since(start) > 30*time.Second {
		t.Error("instant success took suspiciously long")
	}
}

func TestLockedKeychainFailsFast(t *testing.T) {
	r := run(t, true, "FAKE_KEYCHAIN=locked")
	mustFail(t, r, "keychain is locked")
	if r.log != "" {
		t.Error("codesign must not be invoked on a locked keychain")
	}
}

func TestCodesignFailureLeavesBinaryUntouched(t *testing.T) {
	mustFail(t, run(t, true, "FAKE_CODESIGN=fail"), "codesign failed")
}

func TestCodesignTimeoutMapsToClearError(t *testing.T) {
	// Deterministic: the stub raises SIGALRM on itself, exactly the signal
	// the perl alarm delivers at the deadline.
	mustFail(t, run(t, true, "FAKE_CODESIGN=alarm"), "timed out")
}

func TestCodesignRealAlarmKillsHang(t *testing.T) {
	if testing.Short() {
		t.Skip("waits one real second for the alarm")
	}
	start := time.Now()
	r := run(t, true, "FAKE_CODESIGN=hang", "IRIS_CODESIGN_TIMEOUT=1")
	mustFail(t, r, "timed out")
	if time.Since(start) > 120*time.Second {
		t.Error("hang was not killed by the alarm")
	}
}

func TestNoIdentityBuildsAdHocWithWarning(t *testing.T) {
	r := run(t, true, "FAKE_IDENTITY=none")
	if r.err != nil || r.dest != "NEW\n" || r.log != "" {
		t.Fatalf("err=%v dest=%q log=%q", r.err, r.dest, r.log)
	}
	if !strings.Contains(r.out, "WARNING") {
		t.Errorf("expected a warning, got %q", r.out)
	}
}

func TestSkipSignBuildsUnsignedEvenWhenLocked(t *testing.T) {
	r := run(t, true, "IRIS_SKIP_SIGN=1", "FAKE_KEYCHAIN=locked")
	if r.err != nil || r.dest != "NEW\n" || r.log != "" {
		t.Fatalf("err=%v dest=%q log=%q", r.err, r.dest, r.log)
	}
}

func TestMissingCodesignSkipsCleanly(t *testing.T) {
	r := run(t, false)
	if r.err != nil || r.dest != "NEW\n" {
		t.Fatalf("err=%v dest=%q", r.err, r.dest)
	}
	if !strings.Contains(r.out, "codesign not found") {
		t.Errorf("output = %q", r.out)
	}
}

func TestBuildFailureLeavesBinaryUntouched(t *testing.T) {
	r := run(t, true, "IRIS_BUILD_CMD=/usr/bin/false")
	mustFail(t, r, "build failed")
}
