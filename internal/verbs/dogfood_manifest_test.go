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

// TestDogfoodManifestPreviousManifestAbsentOnFirstWrite verifies that the
// first write of a manifest produces a JSON file with NO `previous_manifest`
// key at all — absent, not null, not present-but-empty. This is the
// omitempty-on-pointer contract: a nil *DogfoodManifest marshals as missing.
func TestDogfoodManifestPreviousManifestAbsentOnFirstWrite(t *testing.T) {
	dir := t.TempDir()
	in := sampleManifest()
	if err := WriteManifest(dir, in); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, DogfoodManifestFilename))
	if err != nil {
		t.Fatalf("read raw file: %v", err)
	}
	// Decode into a generic map so we can assert key absence (vs presence-with-null).
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	if _, present := generic["previous_manifest"]; present {
		t.Errorf("first write must NOT include previous_manifest key (absent, not null). raw: %s", raw)
	}

	// And the in-memory round-trip should also report nil.
	got, err := ReadManifest(dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got.PreviousManifest != nil {
		t.Errorf("PreviousManifest should be nil on first write, got: %+v", got.PreviousManifest)
	}
}

// TestDogfoodManifestPreviousManifestEmbedsPriorOnSecondWrite verifies that
// the second write of a manifest embeds the first manifest's full contents
// under previous_manifest.
func TestDogfoodManifestPreviousManifestEmbedsPriorOnSecondWrite(t *testing.T) {
	dir := t.TempDir()

	// First manifest: A.
	a := &DogfoodManifest{
		Base:    ManifestBase{Ref: "main", SHA: "aaa111"},
		Layered: []LayeredEntry{{Name: "F1", SHA: "fff111", Applied: "cherry-pick"}},
		Note:    "first compose",
	}
	if err := WriteManifest(dir, a); err != nil {
		t.Fatalf("WriteManifest A: %v", err)
	}
	aRecordedAt := a.RecordedAt
	if aRecordedAt == "" {
		t.Fatal("expected A.RecordedAt populated after first write")
	}

	// Second manifest: B.
	b := &DogfoodManifest{
		Base:    ManifestBase{Ref: "main", SHA: "bbb222"},
		Layered: []LayeredEntry{{Name: "F2", SHA: "fff222", Applied: "merge"}},
		Note:    "second compose",
	}
	if err := WriteManifest(dir, b); err != nil {
		t.Fatalf("WriteManifest B: %v", err)
	}

	got, err := ReadManifest(dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got.PreviousManifest == nil {
		t.Fatal("PreviousManifest should be populated after second write")
	}
	if got.PreviousManifest.Base != a.Base {
		t.Errorf("prior base mismatch: got %+v want %+v", got.PreviousManifest.Base, a.Base)
	}
	if len(got.PreviousManifest.Layered) != 1 || got.PreviousManifest.Layered[0] != a.Layered[0] {
		t.Errorf("prior layered mismatch: got %+v want %+v", got.PreviousManifest.Layered, a.Layered)
	}
	if got.PreviousManifest.Note != a.Note {
		t.Errorf("prior note mismatch: got %q want %q", got.PreviousManifest.Note, a.Note)
	}
	// The embedded prior keeps its own original RecordedAt — it is NOT overwritten
	// with the second manifest's timestamp.
	if got.PreviousManifest.RecordedAt != aRecordedAt {
		t.Errorf("embedded prior RecordedAt should equal A's original %q, got %q", aRecordedAt, got.PreviousManifest.RecordedAt)
	}
	// The embedded prior must NOT itself carry a previous_manifest (one-deep only).
	if got.PreviousManifest.PreviousManifest != nil {
		t.Errorf("embedded prior must have PreviousManifest nil, got: %+v", got.PreviousManifest.PreviousManifest)
	}

	// Raw-JSON assertion: the embedded prior has no `previous_manifest` key.
	raw, err := os.ReadFile(filepath.Join(dir, DogfoodManifestFilename))
	if err != nil {
		t.Fatalf("read raw file: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	prev, ok := generic["previous_manifest"].(map[string]any)
	if !ok {
		t.Fatalf("expected previous_manifest object in raw JSON, got: %v", generic["previous_manifest"])
	}
	if _, present := prev["previous_manifest"]; present {
		t.Errorf("embedded previous_manifest must not itself contain a previous_manifest key. raw: %s", raw)
	}
}

// TestDogfoodManifestPreviousManifestDepthBoundedAtOne verifies the
// strip-then-embed sequence: after three sequential writes A → B → C, the
// on-disk manifest is C with previous_manifest=B, and B's own
// previous_manifest is nil (not nested A).
func TestDogfoodManifestPreviousManifestDepthBoundedAtOne(t *testing.T) {
	dir := t.TempDir()

	a := &DogfoodManifest{
		Base: ManifestBase{Ref: "main", SHA: "aaa"},
		Note: "A",
	}
	if err := WriteManifest(dir, a); err != nil {
		t.Fatalf("WriteManifest A: %v", err)
	}

	b := &DogfoodManifest{
		Base: ManifestBase{Ref: "main", SHA: "bbb"},
		Note: "B",
	}
	if err := WriteManifest(dir, b); err != nil {
		t.Fatalf("WriteManifest B: %v", err)
	}
	bRecordedAt := b.RecordedAt

	c := &DogfoodManifest{
		Base: ManifestBase{Ref: "main", SHA: "ccc"},
		Note: "C",
	}
	if err := WriteManifest(dir, c); err != nil {
		t.Fatalf("WriteManifest C: %v", err)
	}

	got, err := ReadManifest(dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	// Top level is C.
	if got.Note != "C" || got.Base.SHA != "ccc" {
		t.Errorf("top level should be C, got: note=%q base.sha=%q", got.Note, got.Base.SHA)
	}
	// One step back is B.
	if got.PreviousManifest == nil {
		t.Fatal("PreviousManifest should be B after third write")
	}
	if got.PreviousManifest.Note != "B" || got.PreviousManifest.Base.SHA != "bbb" {
		t.Errorf("PreviousManifest should be B, got: note=%q base.sha=%q",
			got.PreviousManifest.Note, got.PreviousManifest.Base.SHA)
	}
	if got.PreviousManifest.RecordedAt != bRecordedAt {
		t.Errorf("PreviousManifest.RecordedAt should equal B's original %q, got %q",
			bRecordedAt, got.PreviousManifest.RecordedAt)
	}
	// CRITICAL: B's own PreviousManifest must be nil, not A. One-deep only.
	if got.PreviousManifest.PreviousManifest != nil {
		t.Errorf("PreviousManifest.PreviousManifest must be nil (one-deep), got: %+v",
			got.PreviousManifest.PreviousManifest)
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
