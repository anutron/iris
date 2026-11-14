package config

import (
	"fmt"
	"math"
	"net/url"
)

// DogfoodReadyBlock describes project-wide service readiness after restart.
type DogfoodReadyBlock struct {
	Ports           []int    `toml:"ports" json:"ports,omitempty" kind:"shared"`
	HealthURLs      []string `toml:"health_urls" json:"health_urls,omitempty" kind:"shared"`
	TimeoutSeconds  int      `toml:"timeout_seconds" json:"timeout_seconds,omitempty" kind:"shared"`
	IntervalSeconds float64  `toml:"interval_seconds" json:"interval_seconds,omitempty" kind:"shared"`
}

func (b *DogfoodReadyBlock) validate() []ValidationError {
	if b == nil {
		return nil
	}
	var errs []ValidationError
	for i, p := range b.Ports {
		if p < 1 || p > 65535 {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("dogfood_ready.ports[%d]", i), Message: "must be between 1 and 65535"})
		}
	}
	for i, s := range b.HealthURLs {
		u, err := url.Parse(s)
		if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("dogfood_ready.health_urls[%d]", i), Message: "must be an HTTP(S) URL with a host and no credentials"})
		}
	}
	if b.TimeoutSeconds < 0 || b.TimeoutSeconds > 86400 {
		errs = append(errs, ValidationError{Field: "dogfood_ready.timeout_seconds", Message: "must be between 0 and 86400 (0 defaults to 120)"})
	}
	if b.IntervalSeconds < 0 || b.IntervalSeconds > 86400 || math.IsNaN(b.IntervalSeconds) || math.IsInf(b.IntervalSeconds, 0) {
		errs = append(errs, ValidationError{Field: "dogfood_ready.interval_seconds", Message: "must be between 0 and 86400 (0 defaults to 1)"})
	}
	return errs
}
