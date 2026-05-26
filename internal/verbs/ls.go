package verbs

import (
	"context"
	"fmt"
	"time"
)

// LsInput is the public input shape for iris:ls.
type LsInput struct {
	Limit int    // 0 → DefaultLsLimit
	Since string // RFC3339 timestamp, empty → no filter
}

// LsResult is the structured result.
type LsResult struct {
	Entries  []AggregateEntry `json:"entries"`
	Warnings []string         `json:"warnings"`
}

// DefaultLsLimit caps the result count when the caller omits Limit.
const DefaultLsLimit = 50

// Ls reads the audit log and projects managed systems iris has reloaded
// recently. No registry, no scan; the audit log IS the inventory.
//
// The verb is read-only and target-less; no source repo is resolved.
func Ls(ctx context.Context, in LsInput) (*LsResult, error) {
	opts := AuditReadOpts{}
	if in.Since != "" {
		t, err := time.Parse(time.RFC3339, in.Since)
		if err != nil {
			return nil, fmt.Errorf("parse since=%q as RFC3339: %w", in.Since, err)
		}
		opts.Since = t
	}

	entries, err := ReadAudit(opts)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return &LsResult{
			Entries:  []AggregateEntry{},
			Warnings: []string{"no reloads recorded yet"},
		}, nil
	}

	agg := AggregateBySourceRepo(entries)
	limit := in.Limit
	if limit <= 0 {
		limit = DefaultLsLimit
	}
	if len(agg) > limit {
		agg = agg[:limit]
	}
	return &LsResult{
		Entries:  agg,
		Warnings: []string{},
	}, nil
}
