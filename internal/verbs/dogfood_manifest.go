package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DogfoodManifestFilename is the basename of the persisted dogfood manifest,
// written alongside the audit log in the per-source-repo state directory.
const DogfoodManifestFilename = "dogfood-manifest.json"

// DogfoodManifest is the structured record of what composes the dogfood
// branch's current SHA. The agent supplies base + layered + note; iris stamps
// RecordedAt at write time so the manifest is self-describing.
type DogfoodManifest struct {
	Base       ManifestBase   `json:"base"`
	Layered    []LayeredEntry `json:"layered"`
	Note       string         `json:"note,omitempty"`
	RecordedAt string         `json:"recorded_at"` // ISO-8601 UTC, stamped by WriteManifest
}

// ManifestBase names the upstream base the dogfood SHA was composed on top of.
type ManifestBase struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// LayeredEntry describes one branch composed into the dogfood SHA. Applied is
// descriptive only ("cherry-pick", "merge", ...) and is not validated by iris.
type LayeredEntry struct {
	Name    string `json:"name"`
	SHA     string `json:"sha"`
	Applied string `json:"applied,omitempty"`
}

// manifestPath returns the absolute path to the manifest inside stateDir.
func manifestPath(stateDir string) string {
	return filepath.Join(stateDir, DogfoodManifestFilename)
}

// WriteManifest persists m as dogfood-manifest.json inside stateDir,
// atomically: it writes to a sibling ".tmp" file, fsyncs it, then renames over
// the destination. A crash mid-write leaves either the prior file or nothing —
// never a partial file.
//
// WriteManifest always stamps RecordedAt to the current UTC time in
// RFC3339Nano, overwriting whatever the caller passed in, and reflects that
// value back on the passed-in struct so the caller can echo it without
// re-reading. Layered serializes as [] (never null) when empty.
func WriteManifest(stateDir string, m *DogfoodManifest) error {
	if m == nil {
		return fmt.Errorf("manifest: nil manifest")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("manifest: mkdir %s: %w", stateDir, err)
	}

	m.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if m.Layered == nil {
		m.Layered = []LayeredEntry{}
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("manifest: marshal: %w", err)
	}

	dst := manifestPath(stateDir)
	tmp := dst + ".tmp"

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("manifest: open tmp %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("manifest: write tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("manifest: fsync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("manifest: close tmp: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("manifest: rename %s -> %s: %w", tmp, dst, err)
	}
	return nil
}

// ReadManifest loads the manifest from stateDir. It returns (nil, nil) when no
// manifest file exists, and an error on read or parse failure.
func ReadManifest(stateDir string) (*DogfoodManifest, error) {
	path := manifestPath(stateDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("manifest: read %s: %w", path, err)
	}
	var m DogfoodManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest: parse %s: %w", path, err)
	}
	if m.Layered == nil {
		m.Layered = []LayeredEntry{}
	}
	return &m, nil
}
