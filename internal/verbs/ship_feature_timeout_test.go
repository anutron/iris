package verbs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestShipCITimeout_ReadsLocalToml covers Bug 1 for ship_feature:
// ship_ci_timeout_seconds is a local-tagged field, so a value set only in the
// gitignored .iris.local.toml must be honored via the merged overlay.
func TestShipCITimeout_ReadsLocalToml(t *testing.T) {
	// Guard against a leaked override from another ship test in this package.
	old := shipCITimeoutOverride
	shipCITimeoutOverride = nil
	t.Cleanup(func() { shipCITimeoutOverride = old })

	src, _, _, _, _ := setupDogfoodRepoFiles(t, "ship-localtimeout", map[string]string{
		".iris.toml": "schema_version = 1\n[build]\ncommand = [\"true\"]\n[restart]\nmechanism = \"none\"\n",
		".gitignore": ".iris.local.toml\n",
	})
	if err := os.WriteFile(filepath.Join(src, ".iris.local.toml"), []byte("ship_ci_timeout_seconds = 900\n"), 0o644); err != nil {
		t.Fatalf("write .iris.local.toml: %v", err)
	}

	if got := shipCITimeout(src); got != 900*time.Second {
		t.Fatalf("shipCITimeout: got %v want 900s (from .iris.local.toml overlay)", got)
	}
}

// TestShipCITimeout_DefaultWhenUnset confirms the 600s default when neither
// file sets the field.
func TestShipCITimeout_DefaultWhenUnset(t *testing.T) {
	old := shipCITimeoutOverride
	shipCITimeoutOverride = nil
	t.Cleanup(func() { shipCITimeoutOverride = old })

	src, _, _, _, _ := setupDogfoodRepoFiles(t, "ship-defaulttimeout", map[string]string{
		".iris.toml": "schema_version = 1\n[build]\ncommand = [\"true\"]\n[restart]\nmechanism = \"none\"\n",
	})

	if got := shipCITimeout(src); got != 600*time.Second {
		t.Fatalf("shipCITimeout: got %v want the 600s default", got)
	}
}
