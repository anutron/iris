package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Defaults for the `[dogfood_lease]` block, in seconds.
const (
	DefaultLeaseWarmerSeconds      = 600
	DefaultLeaseGraceSeconds       = 30
	DefaultLeaseIdleReleaseSeconds = 1800
	DefaultLeaseInFlightMaxSeconds = 900
)

// DogfoodLeaseBlock is the optional `[dogfood_lease]` block. Each key is a
// pointer so "absent" (use the default) is distinguishable from an explicit
// zero (invalid).
type DogfoodLeaseBlock struct {
	WarmerSeconds      *int `toml:"warmer_seconds"       json:"warmer_seconds,omitempty"`
	GraceSeconds       *int `toml:"grace_seconds"        json:"grace_seconds,omitempty"`
	IdleReleaseSeconds *int `toml:"idle_release_seconds" json:"idle_release_seconds,omitempty"`
	// InFlightMaxSeconds caps how long a holder's in-flight (deploy running)
	// mark is honored, so a killed CLI deploy cannot pin the lease forever.
	InFlightMaxSeconds *int `toml:"in_flight_max_seconds" json:"in_flight_max_seconds,omitempty"`
}

// DogfoodLeaseTimers is the effective timer set after defaults are applied.
type DogfoodLeaseTimers struct {
	WarmerSeconds      int `json:"warmer_seconds"`
	GraceSeconds       int `json:"grace_seconds"`
	IdleReleaseSeconds int `json:"idle_release_seconds"`
	InFlightMaxSeconds int `json:"in_flight_max_seconds"`
}

// DefaultDogfoodLeaseTimers returns the built-in defaults (600/30/1800).
func DefaultDogfoodLeaseTimers() DogfoodLeaseTimers {
	return DogfoodLeaseTimers{
		WarmerSeconds:      DefaultLeaseWarmerSeconds,
		GraceSeconds:       DefaultLeaseGraceSeconds,
		IdleReleaseSeconds: DefaultLeaseIdleReleaseSeconds,
		InFlightMaxSeconds: DefaultLeaseInFlightMaxSeconds,
	}
}

// DogfoodLeaseTimers returns the block's values with defaults filled in for
// missing keys. It does not validate; callers needing a safe set use
// ValidDogfoodLeaseTimers.
func (c IrisToml) DogfoodLeaseTimers() DogfoodLeaseTimers {
	return c.DogfoodLease.timers()
}

func (b *DogfoodLeaseBlock) timers() DogfoodLeaseTimers {
	t := DefaultDogfoodLeaseTimers()
	if b == nil {
		return t
	}
	if b.WarmerSeconds != nil {
		t.WarmerSeconds = *b.WarmerSeconds
	}
	if b.GraceSeconds != nil {
		t.GraceSeconds = *b.GraceSeconds
	}
	if b.IdleReleaseSeconds != nil {
		t.IdleReleaseSeconds = *b.IdleReleaseSeconds
	}
	if b.InFlightMaxSeconds != nil {
		t.InFlightMaxSeconds = *b.InFlightMaxSeconds
	}
	return t
}

// ValidDogfoodLeaseTimers returns the effective timers for b and any
// validation errors. When b is invalid the defaults are returned, so the
// scheduler never runs with nonsensical timers.
func ValidDogfoodLeaseTimers(b *DogfoodLeaseBlock) (DogfoodLeaseTimers, []ValidationError) {
	if errs := b.validate(); len(errs) > 0 {
		return DefaultDogfoodLeaseTimers(), errs
	}
	return b.timers(), nil
}

// EffectiveBlock returns a block with every key populated from t, used by
// validate_config to report resolved values.
func (t DogfoodLeaseTimers) EffectiveBlock() *DogfoodLeaseBlock {
	w, g, i, f := t.WarmerSeconds, t.GraceSeconds, t.IdleReleaseSeconds, t.InFlightMaxSeconds
	return &DogfoodLeaseBlock{WarmerSeconds: &w, GraceSeconds: &g, IdleReleaseSeconds: &i, InFlightMaxSeconds: &f}
}

func (b *DogfoodLeaseBlock) validate() []ValidationError {
	if b == nil {
		return nil
	}
	var errs []ValidationError
	positive := func(name string, v *int) {
		if v != nil && *v <= 0 {
			errs = append(errs, ValidationError{
				Field:   "dogfood_lease." + name,
				Message: "must be a positive integer",
				Hint:    "use a positive number of seconds, or omit the key to use the default",
			})
		}
	}
	positive("warmer_seconds", b.WarmerSeconds)
	positive("grace_seconds", b.GraceSeconds)
	positive("idle_release_seconds", b.IdleReleaseSeconds)
	positive("in_flight_max_seconds", b.InFlightMaxSeconds)
	if len(errs) > 0 {
		return errs
	}
	t := b.timers()
	if t.GraceSeconds >= t.WarmerSeconds {
		errs = append(errs, ValidationError{
			Field:   "dogfood_lease.grace_seconds",
			Message: fmt.Sprintf("grace_seconds (%d) must be less than warmer_seconds (%d)", t.GraceSeconds, t.WarmerSeconds),
			Hint:    "lower grace_seconds or raise warmer_seconds",
		})
	}
	if t.WarmerSeconds > t.IdleReleaseSeconds {
		errs = append(errs, ValidationError{
			Field:   "dogfood_lease.warmer_seconds",
			Message: fmt.Sprintf("warmer_seconds (%d) must be less than or equal to idle_release_seconds (%d)", t.WarmerSeconds, t.IdleReleaseSeconds),
			Hint:    "lower warmer_seconds or raise idle_release_seconds",
		})
	}
	return errs
}

// PeekLocalDogfoodLease leniently reads ONLY the `[dogfood_lease]` block from
// .iris.local.toml at repoRoot. It serves the bootstrap case where
// `.iris.toml` is absent at the source root (as with dogfood_branch). Returns
// nil on any problem.
func PeekLocalDogfoodLease(repoRoot string) *DogfoodLeaseBlock {
	data, err := os.ReadFile(filepath.Join(repoRoot, IrisLocalTomlFilename))
	if err != nil {
		return nil
	}
	var doc IrisToml
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return nil
	}
	return doc.DogfoodLease
}
