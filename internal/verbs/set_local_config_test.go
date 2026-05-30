package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// tomlForLocalConfig is a valid shared .iris.toml (build/restart) used by
// set_local_config tests that need the source repo to have a checked-in
// config alongside the local file. set_local_config itself does NOT require
// .iris.toml to exist — but some scenarios (default_branch lookup) want a
// realistic repo where origin/HEAD is set.
const tomlForLocalConfig = `
schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "none"
`

// setupLocalConfigRepo wires up a bare origin + source clone + argus
// worktree the same way the dogfood tests do, and (optionally) seeds the
// shared .iris.toml so origin/HEAD is unambiguous. Returns (src, wt, client).
func setupLocalConfigRepo(t *testing.T, slug string, sharedToml string) (src, wt string, client *argus.Client) {
	t.Helper()
	setAuditDir(t)
	src, wt, _ = setupRepoWithBareAndWorktree(t, slug)
	if sharedToml != "" {
		g := gitRunner(t)
		if err := os.WriteFile(filepath.Join(src, ".iris.toml"), []byte(sharedToml), 0o644); err != nil {
			t.Fatalf("write .iris.toml: %v", err)
		}
		g(src, "add", ".iris.toml")
		g(src, "commit", "-m", "fixture: .iris.toml")
	}
	client = stubArgus(t, src, wt)
	return
}

// readLocalToml decodes .iris.local.toml at src as a flat map for assertion.
// Missing file returns (nil, nil) — callers check for nil to assert absence.
func readLocalToml(t *testing.T, src string) map[string]any {
	t.Helper()
	path := filepath.Join(src, config.IrisLocalTomlFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read %s: %v", path, err)
	}
	m := map[string]any{}
	if err := tomlDecode(data, &m); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return m
}

// tomlDecode wraps BurntSushi/toml.Decode so callers can pass []byte
// directly and ignore the MetaData side return.
func tomlDecode(data []byte, v any) error {
	_, err := toml.Decode(string(data), v)
	return err
}

func TestSetLocalConfig_SetsFieldInFreshRepo(t *testing.T) {
	src, _, client := setupLocalConfigRepo(t, "slc-fresh", tomlForLocalConfig)

	result, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Fields: map[string]any{"dogfood_branch": "dev"},
	})
	if err != nil {
		t.Fatalf("SetLocalConfig: %v", err)
	}
	if !result.Written {
		t.Fatal("expected Written=true")
	}
	wantPath := filepath.Join(canonicalize(src), config.IrisLocalTomlFilename)
	if result.Path != wantPath {
		t.Fatalf("Path: got %q want %q", result.Path, wantPath)
	}
	if result.Resolved["dogfood_branch"] != "dev" {
		t.Fatalf("Resolved.dogfood_branch: got %v want %q", result.Resolved["dogfood_branch"], "dev")
	}
	on := readLocalToml(t, src)
	if on["dogfood_branch"] != "dev" {
		t.Fatalf("on-disk dogfood_branch: got %v want %q", on["dogfood_branch"], "dev")
	}
}

func TestSetLocalConfig_MergesWithExistingFile(t *testing.T) {
	src, _, client := setupLocalConfigRepo(t, "slc-merge", tomlForLocalConfig)
	// Seed an existing local file.
	seed := `dogfood_branch = "dev"` + "\n" + `ship_ci_timeout_seconds = 900` + "\n"
	if err := os.WriteFile(filepath.Join(src, config.IrisLocalTomlFilename), []byte(seed), 0o644); err != nil {
		t.Fatalf("seed local file: %v", err)
	}

	result, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Fields: map[string]any{"dogfood_branch": "scratch"},
	})
	if err != nil {
		t.Fatalf("SetLocalConfig: %v", err)
	}
	if result.Resolved["dogfood_branch"] != "scratch" {
		t.Fatalf("dogfood_branch: got %v want scratch", result.Resolved["dogfood_branch"])
	}
	if got := result.Resolved["ship_ci_timeout_seconds"]; toInt64(got) != 900 {
		t.Fatalf("ship_ci_timeout_seconds: got %v want 900", got)
	}
	on := readLocalToml(t, src)
	if on["dogfood_branch"] != "scratch" || toInt64(on["ship_ci_timeout_seconds"]) != 900 {
		t.Fatalf("on-disk merge failed: %v", on)
	}
}

func TestSetLocalConfig_DeletesField(t *testing.T) {
	src, _, client := setupLocalConfigRepo(t, "slc-del", tomlForLocalConfig)
	seed := `dogfood_branch = "dev"` + "\n" + `ship_ci_timeout_seconds = 900` + "\n"
	if err := os.WriteFile(filepath.Join(src, config.IrisLocalTomlFilename), []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Delete: []string{"ship_ci_timeout_seconds"},
	})
	if err != nil {
		t.Fatalf("SetLocalConfig: %v", err)
	}
	if _, present := result.Resolved["ship_ci_timeout_seconds"]; present {
		t.Fatalf("ship_ci_timeout_seconds should be deleted; got %v", result.Resolved["ship_ci_timeout_seconds"])
	}
	if result.Resolved["dogfood_branch"] != "dev" {
		t.Fatalf("dogfood_branch should remain; got %v", result.Resolved["dogfood_branch"])
	}
	on := readLocalToml(t, src)
	if _, present := on["ship_ci_timeout_seconds"]; present {
		t.Fatalf("on-disk: ship_ci_timeout_seconds should be gone, got %v", on)
	}
	if on["dogfood_branch"] != "dev" {
		t.Fatalf("on-disk: dogfood_branch should remain")
	}
}

func TestSetLocalConfig_RefusesSharedTaggedField(t *testing.T) {
	src, _, client := setupLocalConfigRepo(t, "slc-shared-set", tomlForLocalConfig)
	// No pre-existing local file.
	_, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Fields: map[string]any{"default_branch": "trunk"},
	})
	if err == nil {
		t.Fatal("expected field_not_local refusal")
	}
	var sErr *SetLocalConfigError
	if !errAs(err, &sErr) {
		t.Fatalf("expected *SetLocalConfigError, got %T: %v", err, err)
	}
	if sErr.Code != "field_not_local" {
		t.Fatalf("Code: got %q want field_not_local", sErr.Code)
	}
	if sErr.Field != "default_branch" {
		t.Fatalf("Field: got %q want default_branch", sErr.Field)
	}
	if !strings.Contains(sErr.Hint, ".iris.toml") {
		t.Fatalf("hint should point at .iris.toml: %q", sErr.Hint)
	}
	if on := readLocalToml(t, src); on != nil {
		t.Fatalf("no writes should occur on refusal; got %v", on)
	}
}

func TestSetLocalConfig_RefusesSharedTaggedFieldInDelete(t *testing.T) {
	src, _, client := setupLocalConfigRepo(t, "slc-shared-del", tomlForLocalConfig)
	_, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Delete: []string{"build"},
	})
	if err == nil {
		t.Fatal("expected field_not_local refusal")
	}
	var sErr *SetLocalConfigError
	if !errAs(err, &sErr) {
		t.Fatalf("expected *SetLocalConfigError, got %T: %v", err, err)
	}
	if sErr.Code != "field_not_local" || sErr.Field != "build" {
		t.Fatalf("got Code=%q Field=%q want field_not_local/build", sErr.Code, sErr.Field)
	}
	if on := readLocalToml(t, src); on != nil {
		t.Fatalf("no writes should occur on refusal; got %v", on)
	}
}

func TestSetLocalConfig_RefusesUnknownField(t *testing.T) {
	src, _, client := setupLocalConfigRepo(t, "slc-unknown", tomlForLocalConfig)

	_, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Fields: map[string]any{"dogfodo_brnach": "dev"},
	})
	if err == nil {
		t.Fatal("expected unknown_field refusal")
	}
	var sErr *SetLocalConfigError
	if !errAs(err, &sErr) {
		t.Fatalf("expected *SetLocalConfigError, got %T: %v", err, err)
	}
	if sErr.Code != "unknown_field" {
		t.Fatalf("Code: got %q want unknown_field", sErr.Code)
	}
	if sErr.Field != "dogfodo_brnach" {
		t.Fatalf("Field: got %q want dogfodo_brnach", sErr.Field)
	}
	if !strings.Contains(sErr.Hint, "dogfood_branch") || !strings.Contains(sErr.Hint, "ship_ci_timeout_seconds") {
		t.Fatalf("hint should list valid local fields; got %q", sErr.Hint)
	}
	if on := readLocalToml(t, src); on != nil {
		t.Fatalf("no writes should occur on refusal; got %v", on)
	}
}

func TestSetLocalConfig_ValidatesPerFieldRules(t *testing.T) {
	src, _, client := setupLocalConfigRepo(t, "slc-invalid", tomlForLocalConfig)

	_, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Fields: map[string]any{"dogfood_branch": "no spaces allowed"},
	})
	if err == nil {
		t.Fatal("expected invalid_value refusal")
	}
	var sErr *SetLocalConfigError
	if !errAs(err, &sErr) {
		t.Fatalf("expected *SetLocalConfigError, got %T: %v", err, err)
	}
	if sErr.Code != "invalid_value" {
		t.Fatalf("Code: got %q want invalid_value", sErr.Code)
	}
	if sErr.Field != "dogfood_branch" {
		t.Fatalf("Field: got %q", sErr.Field)
	}
	if !strings.Contains(sErr.Message, "invalid git branch name") {
		t.Fatalf("message should describe invalid branch: %q", sErr.Message)
	}
	if on := readLocalToml(t, src); on != nil {
		t.Fatalf("no writes should occur on refusal; got %v", on)
	}
}

func TestSetLocalConfig_RefusesDogfoodEqualToDefaultBranch(t *testing.T) {
	// Default branch is main (setupRepoWithBareAndWorktree sets origin/HEAD).
	src, _, client := setupLocalConfigRepo(t, "slc-eq-default", tomlForLocalConfig)
	_, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Fields: map[string]any{"dogfood_branch": "main"},
	})
	if err == nil {
		t.Fatal("expected refusal for dogfood_branch equal to default_branch")
	}
	var sErr *SetLocalConfigError
	if !errAs(err, &sErr) {
		t.Fatalf("expected *SetLocalConfigError, got %T: %v", err, err)
	}
	if sErr.Code != "invalid_value" {
		t.Fatalf("Code: got %q want invalid_value", sErr.Code)
	}
	if sErr.Field != "dogfood_branch" {
		t.Fatalf("Field: got %q", sErr.Field)
	}
	if !strings.Contains(sErr.Message, "default_branch") {
		t.Fatalf("message should cite default_branch rule: %q", sErr.Message)
	}
	if on := readLocalToml(t, src); on != nil {
		t.Fatalf("no writes should occur on refusal; got %v", on)
	}
}

func TestSetLocalConfig_AtomicWrite(t *testing.T) {
	src, _, client := setupLocalConfigRepo(t, "slc-atomic", tomlForLocalConfig)

	// Seed an existing file we'll watch for atomic replacement.
	seed := `dogfood_branch = "dev"` + "\n"
	if err := os.WriteFile(filepath.Join(src, config.IrisLocalTomlFilename), []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Fields: map[string]any{"dogfood_branch": "scratch"},
	})
	if err != nil {
		t.Fatalf("SetLocalConfig: %v", err)
	}
	// The temp file MUST NOT remain after success.
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".iris.local.toml.") && strings.HasSuffix(name, ".tmp") {
			t.Fatalf("temp file %s should have been renamed away", name)
		}
		// Sibling tmp file may also be named with a generic .tmp suffix.
		if strings.HasPrefix(name, ".iris.local.toml") && strings.HasSuffix(name, ".tmp") {
			t.Fatalf("temp file %s should have been renamed away", name)
		}
	}
	// File should still exist and have the new value.
	on := readLocalToml(t, src)
	if on["dogfood_branch"] != "scratch" {
		t.Fatalf("on-disk dogfood_branch: got %v want scratch", on["dogfood_branch"])
	}
}

func TestSetLocalConfig_IdempotentReSet(t *testing.T) {
	src, _, client := setupLocalConfigRepo(t, "slc-idempotent", tomlForLocalConfig)
	seed := `dogfood_branch = "dev"` + "\n"
	if err := os.WriteFile(filepath.Join(src, config.IrisLocalTomlFilename), []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	result, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Fields: map[string]any{"dogfood_branch": "dev"},
	})
	if err != nil {
		t.Fatalf("SetLocalConfig: %v", err)
	}
	if !result.Written {
		t.Fatal("expected Written=true for idempotent re-set")
	}
	on := readLocalToml(t, src)
	if on["dogfood_branch"] != "dev" {
		t.Fatalf("on-disk dogfood_branch: got %v", on["dogfood_branch"])
	}
}

func TestSetLocalConfig_LockSerializesConcurrentCalls(t *testing.T) {
	src, _, client := setupLocalConfigRepo(t, "slc-lock", tomlForLocalConfig)

	// Hold the source-repo lock externally to prove SetLocalConfig parks on it.
	canon, _ := filepath.EvalSymlinks(src)
	mu := lockSourceRepo(canon)
	released := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
			Fields: map[string]any{"dogfood_branch": "dev"},
		})
		if err != nil {
			t.Errorf("SetLocalConfig under lock: %v", err)
		}
		close(released)
	}()
	select {
	case <-released:
		mu.Unlock()
		t.Fatal("SetLocalConfig did not block on the held source-repo lock")
	case <-time.After(150 * time.Millisecond):
		// expected: parked on lockSourceRepo
	}
	mu.Unlock()
	wg.Wait()
}

func TestSetLocalConfig_BothFieldsAndDelete(t *testing.T) {
	src, _, client := setupLocalConfigRepo(t, "slc-both", tomlForLocalConfig)
	seed := `dogfood_branch = "dev"` + "\n" + `ship_ci_timeout_seconds = 900` + "\n"
	if err := os.WriteFile(filepath.Join(src, config.IrisLocalTomlFilename), []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Fields: map[string]any{"dogfood_branch": "scratch"},
		Delete: []string{"ship_ci_timeout_seconds"},
	})
	if err != nil {
		t.Fatalf("SetLocalConfig: %v", err)
	}
	if result.Resolved["dogfood_branch"] != "scratch" {
		t.Fatalf("Resolved.dogfood_branch: got %v", result.Resolved["dogfood_branch"])
	}
	if _, present := result.Resolved["ship_ci_timeout_seconds"]; present {
		t.Fatalf("ship_ci_timeout_seconds should be deleted; got %v", result.Resolved["ship_ci_timeout_seconds"])
	}
	on := readLocalToml(t, src)
	if on["dogfood_branch"] != "scratch" {
		t.Fatalf("on-disk dogfood_branch: got %v", on["dogfood_branch"])
	}
	if _, present := on["ship_ci_timeout_seconds"]; present {
		t.Fatalf("on-disk ship_ci_timeout_seconds should be gone")
	}
}

// TestSetLocalConfig_ResultMarshalsAsJSON covers the "Direct CLI invocation
// mirrors MCP" scenario at the data layer: the verb's result serializes to
// the documented pretty-printed JSON shape both surfaces print.
func TestSetLocalConfig_ResultMarshalsAsJSON(t *testing.T) {
	_, _, client := setupLocalConfigRepo(t, "slc-json", tomlForLocalConfig)

	result, err := SetLocalConfig(context.Background(), client, "task-slc", SetLocalConfigOpts{
		Fields: map[string]any{"dogfood_branch": "dev"},
	})
	if err != nil {
		t.Fatalf("SetLocalConfig: %v", err)
	}
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	for _, key := range []string{`"written"`, `"path"`, `"resolved"`, `"warnings"`} {
		if !strings.Contains(string(body), key) {
			t.Fatalf("result JSON missing %s:\n%s", key, body)
		}
	}
}

// errAs is a tiny wrapper around errors.As so the assertion line stays
// readable in each test (and so a single import of "errors" can be
// localized here).
func errAs(err error, target any) bool { return errors.As(err, target) }

// toInt64 normalises numeric values pulled from a toml.Decode'd
// map[string]any, where ints land as int64 but JSON-derived values might
// land as float64. Both shapes appear in this test file via different
// codepaths.
func toInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	}
	return -1
}
