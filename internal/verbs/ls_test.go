package verbs

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLs_EmptyAuditLog(t *testing.T) {
	setAuditDir(t)
	res, err := Ls(context.Background(), LsInput{})
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if len(res.Entries) != 0 {
		t.Fatalf("expected zero entries, got %d", len(res.Entries))
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "no reloads recorded") {
		t.Fatalf("expected warning about empty log: %+v", res.Warnings)
	}
}

func TestLs_SortsAndAggregates(t *testing.T) {
	setAuditDir(t)
	base := time.Now().UTC()
	// /A: 3 reloads (1 failure), latest 1h ago
	_ = AppendAudit(AuditEntry{Timestamp: base.Add(-3 * time.Hour), TargetSourceRepo: "/A", Outcome: "success"})
	_ = AppendAudit(AuditEntry{Timestamp: base.Add(-2 * time.Hour), TargetSourceRepo: "/A", Outcome: "failure"})
	_ = AppendAudit(AuditEntry{Timestamp: base.Add(-1 * time.Hour), TargetSourceRepo: "/A", Outcome: "success"})
	// /B: 1 reload, 1d ago
	_ = AppendAudit(AuditEntry{Timestamp: base.Add(-24 * time.Hour), TargetSourceRepo: "/B", Outcome: "success"})
	// /C: 1 reload, 1m ago (most recent)
	_ = AppendAudit(AuditEntry{Timestamp: base.Add(-1 * time.Minute), TargetSourceRepo: "/C", Outcome: "success"})

	res, err := Ls(context.Background(), LsInput{})
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if len(res.Entries) != 3 {
		t.Fatalf("expected 3 distinct repos, got %d: %+v", len(res.Entries), res.Entries)
	}
	if res.Entries[0].SourceRepo != "/C" || res.Entries[1].SourceRepo != "/A" || res.Entries[2].SourceRepo != "/B" {
		t.Fatalf("unexpected sort: %s %s %s",
			res.Entries[0].SourceRepo, res.Entries[1].SourceRepo, res.Entries[2].SourceRepo)
	}
	// /A aggregates: 3 reloads, 1 failure.
	for _, e := range res.Entries {
		if e.SourceRepo == "/A" {
			if e.TotalReloadCount != 3 {
				t.Fatalf("/A total: %d", e.TotalReloadCount)
			}
			if e.TotalFailureCount != 1 {
				t.Fatalf("/A failures: %d", e.TotalFailureCount)
			}
		}
	}
}

func TestLs_LimitCapsResults(t *testing.T) {
	setAuditDir(t)
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = AppendAudit(AuditEntry{
			Timestamp:        now.Add(time.Duration(-i) * time.Minute),
			TargetSourceRepo: "/repo-" + string(rune('A'+i)),
			Outcome:          "success",
		})
	}
	res, err := Ls(context.Background(), LsInput{Limit: 2})
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 entries (limit=2), got %d", len(res.Entries))
	}
}

func TestLs_SinceFilter(t *testing.T) {
	setAuditDir(t)
	now := time.Now().UTC()
	_ = AppendAudit(AuditEntry{Timestamp: now.Add(-2 * time.Hour), TargetSourceRepo: "/old", Outcome: "success"})
	_ = AppendAudit(AuditEntry{Timestamp: now, TargetSourceRepo: "/new", Outcome: "success"})
	cutoff := now.Add(-1 * time.Hour).Format(time.RFC3339)
	res, err := Ls(context.Background(), LsInput{Since: cutoff})
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if len(res.Entries) != 1 || res.Entries[0].SourceRepo != "/new" {
		t.Fatalf("expected only /new, got: %+v", res.Entries)
	}
}

func TestLs_InvalidSinceErrors(t *testing.T) {
	setAuditDir(t)
	_, err := Ls(context.Background(), LsInput{Since: "not-a-timestamp"})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLs_DefaultLimitApplied(t *testing.T) {
	setAuditDir(t)
	for i := 0; i < 60; i++ {
		_ = AppendAudit(AuditEntry{
			Timestamp:        time.Now().UTC().Add(time.Duration(-i) * time.Second),
			TargetSourceRepo: "/r/" + string(rune(i)),
			Outcome:          "success",
		})
	}
	res, err := Ls(context.Background(), LsInput{})
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if len(res.Entries) > DefaultLsLimit {
		t.Fatalf("expected default limit %d, got %d", DefaultLsLimit, len(res.Entries))
	}
}

func TestLs_NoSideEffects(t *testing.T) {
	dir := setAuditDir(t)
	_ = AppendAudit(AuditEntry{Timestamp: time.Now().UTC(), TargetSourceRepo: "/r", Outcome: "success"})
	beforeFiles, _ := listFiles(dir)
	if _, err := Ls(context.Background(), LsInput{}); err != nil {
		t.Fatalf("ls: %v", err)
	}
	afterFiles, _ := listFiles(dir)
	if len(beforeFiles) != len(afterFiles) {
		t.Fatalf("Ls modified state files: before=%v after=%v", beforeFiles, afterFiles)
	}
}

// listFiles returns directory entries names; helper for side-effect assertion.
func listFiles(dir string) ([]string, error) {
	return readNames(dir)
}
