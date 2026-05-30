package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleManifest returns a manifest with a base, two layered entries, and a
// note for round-trip tests.
func sampleManifest() *DogfoodManifest {
	return &DogfoodManifest{
		Base: ManifestBase{Ref: "main", SHA: "abc123"},
		Layered: []LayeredEntry{
			{Name: "F2", SHA: "def456", Applied: "cherry-pick"},
			{Name: "F3", SHA: "789aaa", Applied: "merge"},
		},
		Note: "dogfooding F2 + F3",
	}
}

func TestDogfoodManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := sampleManifest()

	if err := WriteManifest(dir, in); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	got, err := ReadManifest(dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got == nil {
		t.Fatal("ReadManifest returned nil for a present manifest")
	}
	if got.Base != in.Base {
		t.Errorf("base mismatch: got %+v want %+v", got.Base, in.Base)
	}
	if len(got.Layered) != 2 {
		t.Fatalf("layered len: got %d want 2", len(got.Layered))
	}
	if got.Layered[0] != in.Layered[0] || got.Layered[1] != in.Layered[1] {
		t.Errorf("layered mismatch: got %+v want %+v", got.Layered, in.Layered)
	}
	if got.Note != in.Note {
		t.Errorf("note mismatch: got %q want %q", got.Note, in.Note)
	}
	if got.RecordedAt == "" {
		t.Error("RecordedAt should be populated after round-trip")
	}
}

func TestDogfoodManifestStampsRecordedAt(t *testing.T) {
	dir := t.TempDir()
	in := sampleManifest()
	in.RecordedAt = "" // explicitly empty; WriteManifest must stamp it

	before := time.Now().UTC()
	if err := WriteManifest(dir, in); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	after := time.Now().UTC()

	got, err := ReadManifest(dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got.RecordedAt == "" {
		t.Fatal("RecordedAt was not stamped by WriteManifest")
	}
	ts, err := time.Parse(time.RFC3339Nano, got.RecordedAt)
	if err != nil {
		t.Fatalf("RecordedAt not RFC3339Nano: %q: %v", got.RecordedAt, err)
	}
	if ts.Before(before.Add(-time.Second)) || ts.After(after.Add(time.Second)) {
		t.Errorf("RecordedAt %v outside expected window [%v, %v]", ts, before, after)
	}

	// The in-memory manifest should also reflect the stamped value, so the
	// caller can echo it back without re-reading.
	if in.RecordedAt == "" {
		t.Error("WriteManifest should stamp RecordedAt on the passed-in struct")
	}
}

func TestDogfoodManifestEmptyLayeredSerializesAsArray(t *testing.T) {
	dir := t.TempDir()
	in := &DogfoodManifest{
		Base:    ManifestBase{Ref: "main", SHA: "abc123"},
		Layered: nil, // empty: must serialize as [] not null
	}
	if err := WriteManifest(dir, in); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, DogfoodManifestFilename))
	if err != nil {
		t.Fatalf("read raw file: %v", err)
	}
	if strings.Contains(string(raw), "\"layered\":null") || strings.Contains(string(raw), "\"layered\": null") {
		t.Errorf("layered serialized as null, want []: %s", raw)
	}
	if !strings.Contains(string(raw), "[]") {
		t.Errorf("layered should serialize as [], got: %s", raw)
	}

	// And it should read back as a non-nil empty slice.
	got, err := ReadManifest(dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got.Layered == nil {
		t.Error("layered read back as nil, want empty slice")
	}
	if len(got.Layered) != 0 {
		t.Errorf("layered len: got %d want 0", len(got.Layered))
	}
}

func TestDogfoodManifestReadAbsentReturnsNilNil(t *testing.T) {
	dir := t.TempDir() // no manifest written
	got, err := ReadManifest(dir)
	if err != nil {
		t.Fatalf("ReadManifest on absent file should be nil error, got: %v", err)
	}
	if got != nil {
		t.Errorf("ReadManifest on absent file should return nil manifest, got: %+v", got)
	}
}

func TestDogfoodManifestReadMalformedReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DogfoodManifestFilename)
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seed garbage: %v", err)
	}
	got, err := ReadManifest(dir)
	if err == nil {
		t.Fatal("ReadManifest on malformed JSON should return an error")
	}
	if got != nil {
		t.Errorf("ReadManifest on malformed JSON should return nil manifest, got: %+v", got)
	}
}

func TestDogfoodManifestNoTempFileRemains(t *testing.T) {
	dir := t.TempDir()
	if err := WriteManifest(dir, sampleManifest()); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	tmp := filepath.Join(dir, DogfoodManifestFilename+".tmp")
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("temp file %s should not remain after a successful write (stat err: %v)", tmp, err)
	}
	// The final file must be present and parse cleanly — never partial.
	final := filepath.Join(dir, DogfoodManifestFilename)
	raw, err := os.ReadFile(final)
	if err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	var m DogfoodManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Errorf("final file is not valid JSON (partial write?): %v", err)
	}
}
