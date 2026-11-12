package config

import (
	"path/filepath"
	"strings"
	"testing"
)

const leaseBaseToml = `
schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "none"
`

func TestDogfoodLeaseTimers_Defaults(t *testing.T) {
	var c IrisToml
	got := c.DogfoodLeaseTimers()
	if got.WarmerSeconds != 600 || got.GraceSeconds != 30 || got.IdleReleaseSeconds != 1800 || got.InFlightMaxSeconds != 900 {
		t.Fatalf("defaults = %+v, want 600/30/1800", got)
	}
}

func TestDogfoodLeaseTimers_PartialOverride(t *testing.T) {
	w := 300
	c := IrisToml{DogfoodLease: &DogfoodLeaseBlock{WarmerSeconds: &w}}
	got := c.DogfoodLeaseTimers()
	if got.WarmerSeconds != 300 || got.GraceSeconds != 30 || got.IdleReleaseSeconds != 1800 {
		t.Fatalf("timers = %+v", got)
	}
}

func TestValidate_DogfoodLease(t *testing.T) {
	ip := func(n int) *int { return &n }
	cases := []struct {
		name      string
		block     *DogfoodLeaseBlock
		wantField string // "" = valid
		wantMsg   string
	}{
		{"nil block valid", nil, "", ""},
		{"all set valid", &DogfoodLeaseBlock{WarmerSeconds: ip(300), GraceSeconds: ip(10), IdleReleaseSeconds: ip(900)}, "", ""},
		{"zero grace", &DogfoodLeaseBlock{GraceSeconds: ip(0)}, "dogfood_lease.grace_seconds", "positive"},
		{"negative warmer", &DogfoodLeaseBlock{WarmerSeconds: ip(-1)}, "dogfood_lease.warmer_seconds", "positive"},
		{"zero in-flight cap", &DogfoodLeaseBlock{InFlightMaxSeconds: ip(0)}, "dogfood_lease.in_flight_max_seconds", "positive"},
		{"in-flight cap ok", &DogfoodLeaseBlock{InFlightMaxSeconds: ip(60)}, "", ""},
		{"zero idle", &DogfoodLeaseBlock{IdleReleaseSeconds: ip(0)}, "dogfood_lease.idle_release_seconds", "positive"},
		{"grace == warmer", &DogfoodLeaseBlock{WarmerSeconds: ip(30)}, "dogfood_lease.grace_seconds", "grace_seconds (30) must be less than warmer_seconds (30)"},
		{"warmer > idle", &DogfoodLeaseBlock{WarmerSeconds: ip(2000)}, "dogfood_lease.warmer_seconds", "warmer_seconds (2000) must be less than or equal to idle_release_seconds (1800)"},
		{"warmer == idle ok", &DogfoodLeaseBlock{WarmerSeconds: ip(1800)}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := IrisToml{
				SchemaVersion: 1,
				Build:         BuildBlock{Command: []string{"true"}},
				Restart:       RestartBlock{Mechanism: MechanismNone},
				DogfoodLease:  tc.block,
			}
			var found *ValidationError
			for _, e := range c.Validate(false) {
				if strings.HasPrefix(e.Field, "dogfood_lease") {
					e := e
					found = &e
				}
			}
			if tc.wantField == "" {
				if found != nil {
					t.Fatalf("unexpected error: %+v", *found)
				}
				return
			}
			if found == nil {
				t.Fatalf("expected an error on %s", tc.wantField)
			}
			if found.Field != tc.wantField || !strings.Contains(found.Message, tc.wantMsg) {
				t.Fatalf("got %+v, want field %q msg containing %q", *found, tc.wantField, tc.wantMsg)
			}
		})
	}
}

func TestLoadOverlay_DogfoodLeaseLocalWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), leaseBaseToml)
	writeFile(t, filepath.Join(dir, IrisLocalTomlFilename), "[dogfood_lease]\nwarmer_seconds = 300\n")
	res, err := LoadOverlay(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Doc.DogfoodLease == nil || res.Doc.DogfoodLeaseTimers().WarmerSeconds != 300 {
		t.Fatalf("local dogfood_lease not applied: %+v", res.Doc.DogfoodLease)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}
}

func TestLoadOverlay_DogfoodLeaseInSharedWarns(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, IrisTomlFilename), leaseBaseToml+"\n[dogfood_lease]\nwarmer_seconds = 300\n")
	res, err := LoadOverlay(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	var w *OverlayWarning
	for i := range res.Warnings {
		if res.Warnings[i].Field == "dogfood_lease" {
			w = &res.Warnings[i]
		}
	}
	if w == nil || w.Code != WarnLocalFieldInSharedConfig || w.Hint != "move dogfood_lease to .iris.local.toml" {
		t.Fatalf("warning = %+v", w)
	}
	if res.Doc.DogfoodLeaseTimers().WarmerSeconds != 300 {
		t.Fatal("value must still be honored")
	}
}

func TestPeekLocalDogfoodLease(t *testing.T) {
	dir := t.TempDir()
	if got := PeekLocalDogfoodLease(dir); got != nil {
		t.Fatalf("missing file: got %+v", got)
	}
	writeFile(t, filepath.Join(dir, IrisLocalTomlFilename), "dogfood_branch = \"dev\"\n[dogfood_lease]\ngrace_seconds = 5\n")
	got := PeekLocalDogfoodLease(dir)
	if got == nil || got.GraceSeconds == nil || *got.GraceSeconds != 5 {
		t.Fatalf("got %+v", got)
	}
	writeFile(t, filepath.Join(dir, IrisLocalTomlFilename), "not = [valid")
	if got := PeekLocalDogfoodLease(dir); got != nil {
		t.Fatalf("malformed: got %+v", got)
	}
}
