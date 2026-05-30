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
	"github.com/anutron/iris/internal/config"
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

// writeLocalToml is a small helper for the overlay scenarios below; it writes
// a `.iris.local.toml` alongside the `.iris.toml` that validateFixture
// already laid down.
func writeLocalToml(t *testing.T, src, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(src, ".iris.local.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write .iris.local.toml: %v", err)
	}
}

// findWarning returns the first warning matching code+field, or nil.
func findWarning(warnings []config.OverlayWarning, code, field string) *config.OverlayWarning {
	for i := range warnings {
		w := warnings[i]
		if w.Code == code && w.Field == field {
			return &warnings[i]
		}
	}
	return nil
}

// TestValidateConfig_MissingLocalFileIsValid covers the spec scenario
// "Local file absent is valid": no `.iris.local.toml` at all is the common
// case and MUST produce no warning, error, or "missing local file" message.
func TestValidateConfig_MissingLocalFileIsValid(t *testing.T) {
	src, client := validateFixture(t, `
schema_version = 1
default_branch = "main"
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
	if len(res.Warnings) != 0 {
		t.Fatalf("expected no warnings, got: %+v", res.Warnings)
	}
	for _, w := range res.Warnings {
		if strings.Contains(strings.ToLower(w.Message), "no local file") ||
			strings.Contains(strings.ToLower(w.Message), "missing local") {
			t.Fatalf("unexpected missing-local-file warning: %+v", w)
		}
	}
}

// TestValidateConfig_DogfoodBranchInSharedWarnsWithHint covers the spec
// scenario "dogfood_branch in .iris.toml warns with migration hint":
// `dogfood_branch` set in `.iris.toml` (legacy placement) produces a
// `local_field_in_shared_config` warning with an explicit migration hint,
// the value is still honored, and `valid` stays true.
func TestValidateConfig_DogfoodBranchInSharedWarnsWithHint(t *testing.T) {
	src, client := validateFixture(t, `
schema_version = 1
default_branch = "main"
dogfood_branch = "dev"
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
		t.Fatalf("expected valid=true (warning is non-fatal), got errors: %+v", res.Errors)
	}
	if res.Resolved == nil || res.Resolved.DogfoodBranch != "dev" {
		t.Fatalf("expected resolved.dogfood_branch=dev (graceful migration), got: %+v", res.Resolved)
	}
	w := findWarning(res.Warnings, config.WarnLocalFieldInSharedConfig, "dogfood_branch")
	if w == nil {
		t.Fatalf("expected local_field_in_shared_config warning for dogfood_branch, got: %+v", res.Warnings)
	}
	if w.File != config.IrisTomlFilename {
		t.Fatalf("warning.file = %q, want %q", w.File, config.IrisTomlFilename)
	}
	if !strings.Contains(w.Hint, "move dogfood_branch to .iris.local.toml") {
		t.Fatalf("warning.hint missing migration text, got %q", w.Hint)
	}
}

// TestValidateConfig_DefaultBranchInLocalWarnsAndIgnored covers the spec
// scenario "default_branch in .iris.local.toml warns and is ignored": the
// shared file's value wins for a shared-tagged field, and a
// `shared_field_in_local_config` warning fires.
func TestValidateConfig_DefaultBranchInLocalWarnsAndIgnored(t *testing.T) {
	src, client := validateFixture(t, `
schema_version = 1
default_branch = "main"
[build]
command = ["true"]
[restart]
mechanism = "none"
`)
	writeLocalToml(t, src, `default_branch = "trunk"`+"\n")

	res, err := ValidateConfig(context.Background(), client, ValidateConfigInput{Path: src})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid=true, got errors: %+v", res.Errors)
	}
	if res.Resolved == nil || res.Resolved.DefaultBranch != "main" {
		t.Fatalf("expected resolved.default_branch=main (shared wins), got: %+v", res.Resolved)
	}
	w := findWarning(res.Warnings, config.WarnSharedFieldInLocalConfig, "default_branch")
	if w == nil {
		t.Fatalf("expected shared_field_in_local_config warning for default_branch, got: %+v", res.Warnings)
	}
	if w.File != config.IrisLocalTomlFilename {
		t.Fatalf("warning.file = %q, want %q", w.File, config.IrisLocalTomlFilename)
	}
}

// TestValidateConfig_ShipCITimeoutInSharedWarnsWithHint covers the spec
// scenario "ship_ci_timeout_seconds in .iris.toml warns with migration
// hint": same shape as the dogfood_branch case but for the other local
// field, ensuring the loader's taxonomy is wired through end-to-end (not
// hardcoded to a single field name).
func TestValidateConfig_ShipCITimeoutInSharedWarnsWithHint(t *testing.T) {
	src, client := validateFixture(t, `
schema_version = 1
ship_ci_timeout_seconds = 900
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
	if res.Resolved == nil || res.Resolved.ShipCITimeoutSeconds != 900 {
		t.Fatalf("expected resolved.ship_ci_timeout_seconds=900, got: %+v", res.Resolved)
	}
	w := findWarning(res.Warnings, config.WarnLocalFieldInSharedConfig, "ship_ci_timeout_seconds")
	if w == nil {
		t.Fatalf("expected local_field_in_shared_config warning for ship_ci_timeout_seconds, got: %+v", res.Warnings)
	}
	if w.File != config.IrisTomlFilename {
		t.Fatalf("warning.file = %q, want %q", w.File, config.IrisTomlFilename)
	}
	if !strings.Contains(w.Hint, "move ship_ci_timeout_seconds to .iris.local.toml") {
		t.Fatalf("warning.hint missing migration text, got %q", w.Hint)
	}
}

// TestValidateConfig_BuildBlockInLocalWarnsAndIgnored covers the spec
// scenario "Build block in .iris.local.toml warns and is ignored":
// `[build]` is a shared TABLE field — the same taxonomy rule applies at
// table granularity, not just scalars.
func TestValidateConfig_BuildBlockInLocalWarnsAndIgnored(t *testing.T) {
	src, client := validateFixture(t, `
schema_version = 1
[build]
command = ["make", "build"]
[restart]
mechanism = "none"
`)
	writeLocalToml(t, src, `
[build]
command = ["echo", "local"]
`)
	res, err := ValidateConfig(context.Background(), client, ValidateConfigInput{Path: src})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid=true, got errors: %+v", res.Errors)
	}
	if res.Resolved == nil || len(res.Resolved.Build.Command) != 2 || res.Resolved.Build.Command[0] != "make" {
		t.Fatalf("expected resolved.build.command=[make build] (shared wins), got: %+v", res.Resolved)
	}
	w := findWarning(res.Warnings, config.WarnSharedFieldInLocalConfig, "build")
	if w == nil {
		t.Fatalf("expected shared_field_in_local_config warning for build, got: %+v", res.Warnings)
	}
	if w.File != config.IrisLocalTomlFilename {
		t.Fatalf("warning.file = %q, want %q", w.File, config.IrisLocalTomlFilename)
	}
}

// TestValidateConfig_MalformedLocalFileReportsErrorAndPreservesShared
// covers the spec scenario "Malformed local file surfaces a structured
// error": a broken `.iris.local.toml` produces an error naming the local
// file (with a line number when available), `valid` flips to false, AND
// `.iris.toml`'s fields are NOT silently lost.
func TestValidateConfig_MalformedLocalFileReportsErrorAndPreservesShared(t *testing.T) {
	src, client := validateFixture(t, `
schema_version = 1
default_branch = "main"
[build]
command = ["true"]
[restart]
mechanism = "none"
`)
	writeLocalToml(t, src, "dogfood_branch = [unclosed\n")

	res, err := ValidateConfig(context.Background(), client, ValidateConfigInput{Path: src})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Valid {
		t.Fatalf("expected valid=false for malformed local file, got: %+v", res)
	}
	// Structured error naming the local file.
	var found *config.ValidationError
	for i := range res.Errors {
		e := res.Errors[i]
		if e.Field == config.IrisLocalTomlFilename {
			found = &res.Errors[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a ValidationError naming %q, got: %+v", config.IrisLocalTomlFilename, res.Errors)
	}
	if !strings.Contains(found.Message, "TOML parse error") {
		t.Fatalf("expected TOML parse error message, got %q", found.Message)
	}
	// Note: `res.Resolved` is nil because valid=false. Per the loader's
	// contract, the shared doc isn't silently lost — verify by relaxing
	// validation and pulling fields off the overlay directly is not the
	// verb's job. Instead we check that the overlay loader preserves
	// shared fields when the local file is malformed; the
	// iris_toml_overlay_test.go already pins that behaviour. Here we
	// confirm the verb surfaces the structured error without claiming
	// `.iris.toml`'s fields are missing (no schema_version / build /
	// restart errors are added).
	for _, e := range res.Errors {
		if e.Field == "schema_version" || e.Field == "build.command" || e.Field == "restart.mechanism" {
			t.Fatalf("shared-file fields should not have been lost; saw error %+v", e)
		}
	}
}

// TestValidateConfig_DogfoodLocalOnlyResolvesFromLocal covers the spec
// scenario "Local file overlays a local-tagged field": no `dogfood_branch`
// in `.iris.toml`, set in `.iris.local.toml` — resolved doc carries the
// local value, no warning, valid=true.
func TestValidateConfig_DogfoodLocalOnlyResolvesFromLocal(t *testing.T) {
	src, client := validateFixture(t, `
schema_version = 1
default_branch = "main"
[build]
command = ["true"]
[restart]
mechanism = "none"
`)
	writeLocalToml(t, src, `dogfood_branch = "dev"`+"\n")

	res, err := ValidateConfig(context.Background(), client, ValidateConfigInput{Path: src})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid=true, got errors: %+v", res.Errors)
	}
	if res.Resolved == nil || res.Resolved.DogfoodBranch != "dev" {
		t.Fatalf("expected resolved.dogfood_branch=dev (from local), got: %+v", res.Resolved)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("expected no warnings when taxonomy is respected, got: %+v", res.Warnings)
	}
}

// TestValidateConfig_LegacyDogfoodLocalOverrideEmitsSingleWarning covers
// the spec scenario "Local file overlays a local-tagged field defined in
// shared (legacy)": `dogfood_branch` set in both files. The local wins,
// exactly one `local_field_in_shared_config` warning fires (no second
// warning about the override itself — that IS the intended migration
// path).
func TestValidateConfig_LegacyDogfoodLocalOverrideEmitsSingleWarning(t *testing.T) {
	src, client := validateFixture(t, `
schema_version = 1
default_branch = "main"
dogfood_branch = "shared-default"
[build]
command = ["true"]
[restart]
mechanism = "none"
`)
	writeLocalToml(t, src, `dogfood_branch = "dev"`+"\n")

	res, err := ValidateConfig(context.Background(), client, ValidateConfigInput{Path: src})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid=true, got errors: %+v", res.Errors)
	}
	if res.Resolved == nil || res.Resolved.DogfoodBranch != "dev" {
		t.Fatalf("expected resolved.dogfood_branch=dev (local wins), got: %+v", res.Resolved)
	}
	matching := 0
	for _, w := range res.Warnings {
		if w.Code == config.WarnLocalFieldInSharedConfig && w.Field == "dogfood_branch" {
			matching++
		}
		if w.Code == config.WarnSharedFieldInLocalConfig && w.Field == "dogfood_branch" {
			t.Fatalf("unexpected shared_field_in_local_config warning for local-tagged field: %+v", w)
		}
	}
	if matching != 1 {
		t.Fatalf("expected exactly one local_field_in_shared_config warning, got %d: %+v", matching, res.Warnings)
	}
}
