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
