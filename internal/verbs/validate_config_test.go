package verbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anutron/iris/internal/argus"
)

// validateFixture returns a source repo with .iris.toml present (or
// absent, if body=="") and a stub argus that allowlists it.
func validateFixture(t *testing.T, body string) (string, *argus.Client) {
	t.Helper()
	src := setupRepoOnly(t)
	if body != "" {
		if err := os.WriteFile(filepath.Join(src, ".iris.toml"), []byte(body), 0o644); err != nil {
			t.Fatalf("write .iris.toml: %v", err)
		}
	}
	canon, _ := filepath.EvalSymlinks(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/projects/full" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	// Point executable elsewhere so this isn't treated as self.
	elsewhere := setupRepoOnly(t)
	bin := filepath.Join(elsewhere, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("x"), 0o755)
	old := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = old })
	return src, argus.New(srv.URL, "stub-token")
}

func TestValidateConfig_ValidReturnsTrue(t *testing.T) {
	src, client := validateFixture(t, `
schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "none"
`)
	res, err := ValidateConfig(context.Background(), client, ValidateConfigInput{Path: src})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid=true, got errors: %+v", res.Errors)
	}
	if res.Resolved == nil {
		t.Fatal("expected resolved doc on success")
	}
}

func TestValidateConfig_MissingFileInvalid(t *testing.T) {
	src, client := validateFixture(t, "")
	res, err := ValidateConfig(context.Background(), client, ValidateConfigInput{Path: src})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected valid=false for missing file")
	}
	if len(res.Errors) == 0 || !strings.Contains(res.Errors[0].Message, "file not found") {
		t.Fatalf("expected file-not-found error: %+v", res.Errors)
	}
}

func TestValidateConfig_MalformedReportsLine(t *testing.T) {
	src, client := validateFixture(t, "schema_version = 1\nbroken = [\n")
	res, err := ValidateConfig(context.Background(), client, ValidateConfigInput{Path: src})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected valid=false")
	}
	if len(res.Errors) == 0 || res.Errors[0].Line == 0 {
		t.Fatalf("expected line number: %+v", res.Errors)
	}
}

func TestValidateConfig_MechanismFieldConflictHint(t *testing.T) {
	src, client := validateFixture(t, `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "launchagent"
label = "com.example.x"
pid_file = "/tmp/foo.pid"
`)
	res, err := ValidateConfig(context.Background(), client, ValidateConfigInput{Path: src})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected valid=false")
	}
	found := false
	for _, e := range res.Errors {
		if e.Field == "restart.pid_file" && strings.Contains(e.Hint, "launchagent") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected pid_file conflict with remediation hint: %+v", res.Errors)
	}
}

func TestValidateConfig_NoSideEffects(t *testing.T) {
	src, client := validateFixture(t, `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "none"
`)
	beforeSha := headSHA(t, src)
	setAuditDir(t)
	if _, err := ValidateConfig(context.Background(), client, ValidateConfigInput{Path: src}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if afterSha := headSHA(t, src); afterSha != beforeSha {
		t.Fatalf("expected no git mutation: before=%s after=%s", beforeSha, afterSha)
	}
	entries, _ := ReadAudit(AuditReadOpts{})
	if len(entries) != 0 {
		t.Fatalf("expected no audit entries, got %d", len(entries))
	}
}

func TestValidateConfig_TaskIDOptional(t *testing.T) {
	// no path, no task_id → resolves self. Build a fixture where executable
	// points into an existing repo with .iris.toml so this succeeds.
	src := setupRepoOnly(t)
	if err := os.WriteFile(filepath.Join(src, ".iris.toml"), []byte(`
schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "exit_code"
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	bin := filepath.Join(src, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("x"), 0o755)
	old := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = old })

	res, err := ValidateConfig(context.Background(), nil, ValidateConfigInput{})
	if err != nil {
		t.Fatalf("validate self: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid self, got: %+v", res.Errors)
	}
}
