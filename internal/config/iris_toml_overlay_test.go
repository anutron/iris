package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadOverlay_SharedOnly verifies the no-local-file path: a repo with
// only `.iris.toml` resolves to that file's content, provenance is "shared"
// for every set field, and no overlay warnings are emitted.
func TestLoadOverlay_SharedOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), `
schema_version = 1
default_branch = "main"

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
`)

	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if result.Doc == nil {
		t.Fatalf("expected non-nil doc")
	}
	if result.Doc.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d", result.Doc.SchemaVersion)
	}
	if result.Doc.DefaultBranch != "main" {
		t.Fatalf("default_branch = %q", result.Doc.DefaultBranch)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", result.Warnings)
	}
	if len(result.ValidationErrors) != 0 {
		t.Fatalf("expected no validation errors, got: %v", result.ValidationErrors)
	}
	// Provenance: every field that was set in .iris.toml is "shared".
	if got, want := result.Provenance["schema_version"], SourceShared; got != want {
		t.Fatalf("provenance[schema_version] = %q, want %q", got, want)
	}
	if got, want := result.Provenance["default_branch"], SourceShared; got != want {
		t.Fatalf("provenance[default_branch] = %q, want %q", got, want)
	}
	if got, want := result.Provenance["build"], SourceShared; got != want {
		t.Fatalf("provenance[build] = %q, want %q", got, want)
	}
	if got, want := result.Provenance["restart"], SourceShared; got != want {
		t.Fatalf("provenance[restart] = %q, want %q", got, want)
	}
	// Provenance: a field never set anywhere is omitted.
	if _, present := result.Provenance["dogfood_branch"]; present {
		t.Fatalf("provenance must omit unset field dogfood_branch; got: %v", result.Provenance)
	}
}

// TestLoadOverlay_LocalOnlyField verifies the core overlay path: a local-tagged
// field set only in `.iris.local.toml` ends up in the resolved doc, provenance
// reports "local", and no warning is emitted.
func TestLoadOverlay_LocalOnlyField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), `
schema_version = 1
default_branch = "main"

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
`)
	writeFile(t, filepath.Join(dir, IrisLocalTomlFilename), `
dogfood_branch = "dev"
ship_ci_timeout_seconds = 900
`)

	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", result.Warnings)
	}
	if result.Doc.DogfoodBranch != "dev" {
		t.Fatalf("dogfood_branch = %q, want %q", result.Doc.DogfoodBranch, "dev")
	}
	if result.Doc.ShipCITimeoutSeconds != 900 {
		t.Fatalf("ship_ci_timeout_seconds = %d, want 900", result.Doc.ShipCITimeoutSeconds)
	}
	if got, want := result.Provenance["dogfood_branch"], SourceLocal; got != want {
		t.Fatalf("provenance[dogfood_branch] = %q, want %q", got, want)
	}
	if got, want := result.Provenance["ship_ci_timeout_seconds"], SourceLocal; got != want {
		t.Fatalf("provenance[ship_ci_timeout_seconds] = %q, want %q", got, want)
	}
	// Sanity: shared fields still report "shared".
	if got, want := result.Provenance["default_branch"], SourceShared; got != want {
		t.Fatalf("provenance[default_branch] = %q, want %q", got, want)
	}
}

// TestLoadOverlay_LocalFieldInSharedFileMigrates verifies the graceful-migration
// path: a local-tagged field present in `.iris.toml` (legacy placement) is
// honored AND produces a `local_field_in_shared_config` warning per spec
// scenario "dogfood_branch in .iris.toml warns with migration hint".
func TestLoadOverlay_LocalFieldInSharedFileMigrates(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), `
schema_version = 1
default_branch = "main"
dogfood_branch = "dev"

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
`)
	// No .iris.local.toml.

	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	// Value still honored — graceful migration.
	if result.Doc.DogfoodBranch != "dev" {
		t.Fatalf("dogfood_branch = %q, want %q (value must be honored even when in shared file)", result.Doc.DogfoodBranch, "dev")
	}
	// Warning emitted.
	var got *OverlayWarning
	for i := range result.Warnings {
		w := result.Warnings[i]
		if w.Code == WarnLocalFieldInSharedConfig && w.Field == "dogfood_branch" {
			got = &result.Warnings[i]
		}
	}
	if got == nil {
		t.Fatalf("expected local_field_in_shared_config warning for dogfood_branch; got: %v", result.Warnings)
	}
	if got.File != IrisTomlFilename {
		t.Fatalf("warning file = %q, want %q", got.File, IrisTomlFilename)
	}
	if !strings.Contains(got.Hint, "move dogfood_branch to .iris.local.toml") {
		t.Fatalf("warning hint should contain migration text, got %q", got.Hint)
	}
	// Provenance: the value came from the shared file even though it's a local-tagged field.
	if got, want := result.Provenance["dogfood_branch"], SourceShared; got != want {
		t.Fatalf("provenance[dogfood_branch] = %q, want %q (value lives in .iris.toml)", got, want)
	}
}

// TestLoadOverlay_LocalFieldInBothFilesLocalWins verifies the migration
// override path: when `.iris.toml` contains a legacy `dogfood_branch` AND
// `.iris.local.toml` also sets it, the local file's value wins (per spec
// "Local file overlays a local-tagged field defined in shared (legacy)").
// One local_field_in_shared_config warning fires; no second warning about the
// override itself — that IS the intended migration path.
func TestLoadOverlay_LocalFieldInBothFilesLocalWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), `
schema_version = 1
default_branch = "main"
dogfood_branch = "shared-default"

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
`)
	writeFile(t, filepath.Join(dir, IrisLocalTomlFilename), `
dogfood_branch = "dev"
`)

	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if result.Doc.DogfoodBranch != "dev" {
		t.Fatalf("dogfood_branch = %q, want %q (local must win for local-tagged fields)", result.Doc.DogfoodBranch, "dev")
	}
	// Exactly one warning, and it's the legacy-placement warning on the shared file.
	matching := 0
	for _, w := range result.Warnings {
		if w.Code == WarnLocalFieldInSharedConfig && w.Field == "dogfood_branch" {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("expected exactly one local_field_in_shared_config warning for dogfood_branch, got %d: %v", matching, result.Warnings)
	}
	// No shared_field_in_local_config warning for dogfood_branch (it's local-tagged).
	for _, w := range result.Warnings {
		if w.Code == WarnSharedFieldInLocalConfig && w.Field == "dogfood_branch" {
			t.Fatalf("unexpected shared_field_in_local_config warning for local-tagged field: %v", w)
		}
	}
	// Provenance reports local (the winning value came from .iris.local.toml).
	if got, want := result.Provenance["dogfood_branch"], SourceLocal; got != want {
		t.Fatalf("provenance[dogfood_branch] = %q, want %q (local won)", got, want)
	}
}

// TestLoadOverlay_SharedFieldInLocalFileWarnsAndIgnored verifies the
// taxonomy-enforcement path for shared-tagged fields: a shared field
// (`default_branch`) set in `.iris.local.toml` is ignored, the shared value
// wins, and a `shared_field_in_local_config` warning is appended (per spec
// scenario "default_branch in .iris.local.toml warns and is ignored").
func TestLoadOverlay_SharedFieldInLocalFileWarnsAndIgnored(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), `
schema_version = 1
default_branch = "main"

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
`)
	writeFile(t, filepath.Join(dir, IrisLocalTomlFilename), `
default_branch = "trunk"
`)

	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	// Shared file's value wins.
	if result.Doc.DefaultBranch != "main" {
		t.Fatalf("default_branch = %q, want %q (shared must win for shared-tagged fields)", result.Doc.DefaultBranch, "main")
	}
	// Warning emitted with the right shape.
	var got *OverlayWarning
	for i := range result.Warnings {
		w := result.Warnings[i]
		if w.Code == WarnSharedFieldInLocalConfig && w.Field == "default_branch" {
			got = &result.Warnings[i]
		}
	}
	if got == nil {
		t.Fatalf("expected shared_field_in_local_config warning for default_branch; got: %v", result.Warnings)
	}
	if got.File != IrisLocalTomlFilename {
		t.Fatalf("warning file = %q, want %q", got.File, IrisLocalTomlFilename)
	}
	// Provenance: value came from the shared file.
	if got, want := result.Provenance["default_branch"], SourceShared; got != want {
		t.Fatalf("provenance[default_branch] = %q, want %q", got, want)
	}
}

// TestLoadOverlay_SharedBlockInLocalFileWarnsAndIgnored mirrors the previous
// test but for a shared TABLE field (`[build]`) rather than a scalar. The
// spec scenario "Build block in .iris.local.toml warns and is ignored"
// requires the same taxonomy behavior at table-level granularity.
func TestLoadOverlay_SharedBlockInLocalFileWarnsAndIgnored(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), `
schema_version = 1

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
`)
	writeFile(t, filepath.Join(dir, IrisLocalTomlFilename), `
[build]
command = ["echo", "local"]
`)

	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	// Shared file's value wins.
	if len(result.Doc.Build.Command) != 2 || result.Doc.Build.Command[0] != "make" {
		t.Fatalf("build.command = %#v, want [make build]", result.Doc.Build.Command)
	}
	// Warning emitted.
	var got *OverlayWarning
	for i := range result.Warnings {
		w := result.Warnings[i]
		if w.Code == WarnSharedFieldInLocalConfig && w.Field == "build" {
			got = &result.Warnings[i]
		}
	}
	if got == nil {
		t.Fatalf("expected shared_field_in_local_config warning for build; got: %v", result.Warnings)
	}
	if got.File != IrisLocalTomlFilename {
		t.Fatalf("warning file = %q, want %q", got.File, IrisLocalTomlFilename)
	}
}

// TestLoadOverlay_MissingLocalFileIsSilent verifies the absence path: no
// `.iris.local.toml` is the common case and MUST NOT produce a warning or
// error. Spec scenario "Local file is silent on absent".
func TestLoadOverlay_MissingLocalFileIsSilent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), `
schema_version = 1
default_branch = "main"

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
`)
	// No .iris.local.toml.

	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if result.Doc == nil {
		t.Fatalf("expected non-nil doc")
	}
	for _, w := range result.Warnings {
		// Specifically: no warning anywhere mentioning the missing local file.
		if strings.Contains(strings.ToLower(w.Message), "no local file") ||
			strings.Contains(strings.ToLower(w.Message), "missing local") {
			t.Fatalf("unexpected missing-local-file warning: %v", w)
		}
	}
	// Provenance for shared-set fields is still shared.
	if got, want := result.Provenance["default_branch"], SourceShared; got != want {
		t.Fatalf("provenance[default_branch] = %q, want %q", got, want)
	}
}

// TestLoadOverlay_MalformedLocalFileSurfacesStructuredError verifies the
// spec scenario "Malformed local file surfaces a structured error":
// `.iris.toml` is valid, `.iris.local.toml` is broken. Expectation: a
// structured ValidationError naming the local file (with a line number when
// the parser supplies one), AND `.iris.toml`'s fields are NOT lost.
func TestLoadOverlay_MalformedLocalFileSurfacesStructuredError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), `
schema_version = 1
default_branch = "main"

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
`)
	writeFile(t, filepath.Join(dir, IrisLocalTomlFilename), "dogfood_branch = [unclosed\n")

	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if result.Doc == nil {
		t.Fatalf(".iris.toml's fields must NOT be lost when local file is malformed")
	}
	// Shared fields still present.
	if result.Doc.DefaultBranch != "main" {
		t.Fatalf("default_branch lost on local-file parse failure: %q", result.Doc.DefaultBranch)
	}
	if result.Doc.SchemaVersion != 1 {
		t.Fatalf("schema_version lost on local-file parse failure: %d", result.Doc.SchemaVersion)
	}
	// Structured error on the local file.
	var found *ValidationError
	for i := range result.ValidationErrors {
		e := result.ValidationErrors[i]
		if e.Field == IrisLocalTomlFilename {
			found = &result.ValidationErrors[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a ValidationError naming %q, got: %v", IrisLocalTomlFilename, result.ValidationErrors)
	}
	if !strings.Contains(found.Message, "TOML parse error") {
		t.Fatalf("error message should describe a TOML parse error, got %q", found.Message)
	}
}

// TestLoadOverlay_ShipCITimeoutLocalOnly verifies the other local-tagged
// field besides dogfood_branch: ship_ci_timeout_seconds set only in
// .iris.local.toml resolves into the doc and provenance reports "local".
func TestLoadOverlay_ShipCITimeoutLocalOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), `
schema_version = 1

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
`)
	writeFile(t, filepath.Join(dir, IrisLocalTomlFilename), `
ship_ci_timeout_seconds = 1200
`)

	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if result.Doc.ShipCITimeoutSeconds != 1200 {
		t.Fatalf("ship_ci_timeout_seconds = %d, want 1200", result.Doc.ShipCITimeoutSeconds)
	}
	if got, want := result.Provenance["ship_ci_timeout_seconds"], SourceLocal; got != want {
		t.Fatalf("provenance[ship_ci_timeout_seconds] = %q, want %q", got, want)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", result.Warnings)
	}
}

// TestLoadOverlay_NoSharedFile verifies the very-empty-repo path: neither file
// exists. The behavior matches LoadIrisToml's no-file path — Doc is nil,
// provenance is empty (not nil), no warnings, no error.
func TestLoadOverlay_NoSharedFile(t *testing.T) {
	dir := t.TempDir()
	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if result.Doc != nil {
		t.Fatalf("expected nil doc when neither file exists, got %+v", result.Doc)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings when neither file exists, got: %v", result.Warnings)
	}
	if result.Provenance == nil {
		t.Fatalf("Provenance must be a non-nil (empty) map, not nil")
	}
	if len(result.Provenance) != 0 {
		t.Fatalf("expected empty provenance when neither file exists, got: %v", result.Provenance)
	}
}

// TestLoadOverlay_LocalOnlyNoSharedFile verifies a degenerate case worth
// pinning: `.iris.local.toml` exists but `.iris.toml` does not. The local
// file alone is not enough — without a shared file, the loader behaves as
// if there were no config (Doc is nil) AND surfaces a structured error.
// We don't synthesize a stub IrisToml from local-only contents.
func TestLoadOverlay_LocalOnlyNoSharedFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisLocalTomlFilename), `
dogfood_branch = "dev"
`)

	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if result.Doc != nil {
		t.Fatalf("expected nil doc when only the local file exists, got %+v", result.Doc)
	}
}

// Helper: tests should be able to construct an explicit absent local file by
// removing it; verify os.Remove paths cleanly so subsequent test assumptions
// about missing-file behavior hold.
func TestLoadOverlay_RemovedLocalFileIsAbsent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), `
schema_version = 1
[build]
command = ["make"]
[restart]
mechanism = "exit_code"
`)
	localPath := filepath.Join(dir, IrisLocalTomlFilename)
	writeFile(t, localPath, `dogfood_branch = "dev"`)
	if err := os.Remove(localPath); err != nil {
		t.Fatalf("remove local file: %v", err)
	}
	result, err := LoadOverlay(dir, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if result.Doc == nil {
		t.Fatalf("expected non-nil doc")
	}
	if result.Doc.DogfoodBranch != "" {
		t.Fatalf("dogfood_branch should be empty after local file removal, got %q", result.Doc.DogfoodBranch)
	}
}
