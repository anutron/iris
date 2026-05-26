package verbs

import (
	"path/filepath"
	"testing"
	"time"
)

// setAuditDir points the audit-log helpers at a tmp dir for the duration
// of the test.
func setAuditDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "audit")
	t.Setenv(AuditDirEnv, dir)
	return dir
}

func TestAppendAndReadAudit_SingleEntry(t *testing.T) {
	setAuditDir(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	entry := AuditEntry{
		Timestamp:        now,
		Caller:           "cli",
		TargetSourceRepo: "/repo/a",
		Mode:             "self",
		Pulled:           true,
		PrePullSha:       "aaa",
		PostPullSha:      "bbb",
		BuildOutput:      "compiled\n",
		RestartMechanism: "exit_code",
		RestartPending:   true,
		Outcome:          "success",
	}
	if err := AppendAudit(entry); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := ReadAudit(AuditReadOpts{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if got[0].Outcome != "success" || got[0].Caller != "cli" || got[0].TargetSourceRepo != "/repo/a" {
		t.Fatalf("unexpected entry: %+v", got[0])
	}
}

func TestReadAudit_MissingFileEmpty(t *testing.T) {
	setAuditDir(t)
	got, err := ReadAudit(AuditReadOpts{})
	if err != nil {
		t.Fatalf("read missing: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestAppendAuditFailureEntry(t *testing.T) {
	setAuditDir(t)
	entry := AuditEntry{
		TargetSourceRepo: "/repo/b",
		Outcome:          "failure",
		FailureReason:    "build exited 1",
	}
	if err := AppendAudit(entry); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, _ := ReadAudit(AuditReadOpts{})
	if len(got) != 1 || got[0].Outcome != "failure" || got[0].FailureReason != "build exited 1" {
		t.Fatalf("unexpected: %+v", got)
	}
	if got[0].Timestamp.IsZero() {
		t.Fatalf("expected timestamp to be auto-set on append")
	}
}

func TestAggregateBySourceRepo(t *testing.T) {
	setAuditDir(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	entries := []AuditEntry{
		{Timestamp: base.Add(-3 * time.Hour), TargetSourceRepo: "/A", Outcome: "success", PostPullSha: "a1"},
		{Timestamp: base.Add(-2 * time.Hour), TargetSourceRepo: "/A", Outcome: "failure", FailureReason: "build", PostPullSha: "a2"},
		{Timestamp: base.Add(-1 * time.Hour), TargetSourceRepo: "/A", Outcome: "success", PostPullSha: "a3"},
		{Timestamp: base.Add(-30 * time.Minute), TargetSourceRepo: "/B", Outcome: "success", PostPullSha: "b1"},
		{Timestamp: base.Add(-10 * time.Minute), TargetSourceRepo: "/C", Outcome: "success", PostPullSha: "c1"},
	}
	for _, e := range entries {
		if err := AppendAudit(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	read, err := ReadAudit(AuditReadOpts{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	agg := AggregateBySourceRepo(read)
	if len(agg) != 3 {
		t.Fatalf("want 3 aggregated, got %d: %+v", len(agg), agg)
	}
	// Sorted descending by LastReloadAt.
	if agg[0].SourceRepo != "/C" || agg[1].SourceRepo != "/B" || agg[2].SourceRepo != "/A" {
		t.Fatalf("expected order C,B,A, got %v %v %v",
			agg[0].SourceRepo, agg[1].SourceRepo, agg[2].SourceRepo)
	}
	var a AggregateEntry
	for _, e := range agg {
		if e.SourceRepo == "/A" {
			a = e
		}
	}
	if a.TotalReloadCount != 3 {
		t.Fatalf("/A total reloads: %d, want 3", a.TotalReloadCount)
	}
	if a.TotalFailureCount != 1 {
		t.Fatalf("/A total failures: %d, want 1", a.TotalFailureCount)
	}
	if a.LastPostPullSha != "a3" {
		t.Fatalf("/A last post_pull_sha = %s, want a3", a.LastPostPullSha)
	}
}

func TestReadAudit_SinceFilter(t *testing.T) {
	setAuditDir(t)
	now := time.Now().UTC()
	if err := AppendAudit(AuditEntry{Timestamp: now.Add(-2 * time.Hour), TargetSourceRepo: "/old"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := AppendAudit(AuditEntry{Timestamp: now, TargetSourceRepo: "/new"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := ReadAudit(AuditReadOpts{Since: now.Add(-1 * time.Hour)})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 || got[0].TargetSourceRepo != "/new" {
		t.Fatalf("expected only /new, got %+v", got)
	}
}

func TestReadAudit_Limit(t *testing.T) {
	setAuditDir(t)
	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = AppendAudit(AuditEntry{Timestamp: base.Add(time.Duration(i) * time.Second), TargetSourceRepo: "/r"})
	}
	got, err := ReadAudit(AuditReadOpts{Limit: 2})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("limit=2 returned %d", len(got))
	}
}

func TestLatestForRepo(t *testing.T) {
	setAuditDir(t)
	base := time.Now().UTC()
	_ = AppendAudit(AuditEntry{Timestamp: base.Add(-2 * time.Hour), TargetSourceRepo: "/x", PostPullSha: "old"})
	_ = AppendAudit(AuditEntry{Timestamp: base.Add(-1 * time.Hour), TargetSourceRepo: "/x", PostPullSha: "newer"})
	_ = AppendAudit(AuditEntry{Timestamp: base, TargetSourceRepo: "/y", PostPullSha: "y1"})

	got, err := LatestForRepo("/x")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got == nil || got.PostPullSha != "newer" {
		t.Fatalf("expected newer entry, got %+v", got)
	}
	got, err = LatestForRepo("/none")
	if err != nil {
		t.Fatalf("latest none: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unseen repo, got %+v", got)
	}
}
