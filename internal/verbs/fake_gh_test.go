package verbs

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFakeGH writes an executable named `gh` into a fresh temp dir whose
// body is scriptBody (a /bin/sh script), prepends that dir to PATH for the
// remainder of the test, and returns the temp dir. Tests use the returned
// dir to inspect side effects the fake gh wrote (e.g., an argv file).
//
// scriptBody runs after a `#!/bin/sh` shebang. The script's $TMPDIR-style
// directory is exported as IRIS_FAKE_GH_DIR so the script can write
// captures back into it.
func writeFakeGH(t *testing.T, scriptBody string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nexport IRIS_FAKE_GH_DIR=\"" + dir + "\"\n" + scriptBody
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

// readFakeGHArgv returns the argv-capture file written by the fake gh.
// Returns "" if the file is absent (i.e., the fake gh was never invoked).
func readFakeGHArgv(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "argv"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read argv: %v", err)
	}
	return string(data)
}

// fakeGHRecordCalls appends every invocation's argv to $IRIS_FAKE_GH_DIR/calls
// as one "CALL <arg> <arg> ..." line. Unlike fakeGHCaptureArgv (which
// overwrites a single argv file), this accumulates across the several gh calls
// pr-auto makes (create, api check-runs, review, merge, view) so tests can
// assert the presence or absence of each.
const fakeGHRecordCalls = `
{ printf 'CALL'; for a in "$@"; do printf ' %s' "$a"; done; printf '\n'; } >> "$IRIS_FAKE_GH_DIR/calls"
`

// readFakeGHCalls returns the accumulated calls log, or "" if the fake gh was
// never invoked.
func readFakeGHCalls(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read calls: %v", err)
	}
	return string(data)
}

// fakeGHPRAutoBody builds a fake-gh script body driving the full pr-auto flow.
// It records every call (fakeGHRecordCalls) and dispatches per subcommand:
//
//	gh pr create ...                            -> emits a PR #88 URL
//	gh api .../commits/<sha>/check-runs          -> emits checksJSON verbatim
//	gh pr review <n> --approve                   -> ok
//	gh pr merge --<method> <n>                   -> ok
//	gh pr view <n> --json mergeCommit            -> a merge commit oid
//
// checksJSON is the GitHub check-runs API response body the fake returns for
// the check-runs query; it MUST NOT contain a single quote.
func fakeGHPRAutoBody(checksJSON string) string {
	return fakeGHRecordCalls + `
case "$1" in
  api)
    printf '%s' '` + checksJSON + `'
    exit 0
    ;;
  pr)
    case "$2" in
      create) printf '%s\n' "https://github.com/anutron/iris/pull/88"; exit 0 ;;
      review) printf '%s\n' "Approved pull request #88"; exit 0 ;;
      merge)  printf '%s\n' "Merged pull request #88"; exit 0 ;;
      view)   printf '%s' '{"mergeCommit":{"oid":"feedface0000c0ffee0000deadbeef0000abcd12"}}'; exit 0 ;;
    esac
    ;;
esac
echo "unexpected gh invocation: $*" >&2
exit 1
`
}
