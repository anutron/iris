package verbs

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// AuditFilename is the basename of the reload-history audit log inside
// AuditDir().
const AuditFilename = "reload-history.jsonl"

// AuditDirEnv lets tests redirect ~/.iris elsewhere without touching the
// caller's home directory. Production code reads $HOME directly.
const AuditDirEnv = "IRIS_AUDIT_DIR"

// AuditEntry is one line in `~/.iris/reload-history.jsonl`. It carries the
// full structured result of a reload plus the wall-clock timestamp and caller
// identity. The schema is appended-to, never reshaped; readers ignore
// unknown fields.
type AuditEntry struct {
	Timestamp        time.Time `json:"timestamp"`
	Caller           string    `json:"caller"`
	TargetSourceRepo string    `json:"target_source_repo"`
	Mode             string    `json:"mode"`
	Pulled           bool      `json:"pulled"`
	PrePullSha       string    `json:"pre_pull_sha"`
	PostPullSha      string    `json:"post_pull_sha"`
	BuildOutput      string    `json:"build_output"`
	RestartMechanism string    `json:"restart_mechanism"`
	RestartOutput    string    `json:"restart_output"`
	PreFlightOutput  string    `json:"pre_flight_output"`
	VerifyOutput     string    `json:"verify_output"`
	RestartPending   bool      `json:"restart_pending"`
	Warnings         []string  `json:"warnings,omitempty"`
	Outcome          string    `json:"outcome"`
	FailureReason    string    `json:"failure_reason,omitempty"`
}

// AggregateEntry collapses many audit entries for one source repo into a
// single ls/status-friendly row.
type AggregateEntry struct {
	SourceRepo         string    `json:"source_repo"`
	LastReloadAt       time.Time `json:"last_reload_at"`
	LastOutcome        string    `json:"last_outcome"`
	LastMode           string    `json:"last_mode"`
	LastPrePullSha     string    `json:"last_pre_pull_sha"`
	LastPostPullSha    string    `json:"last_post_pull_sha"`
	LastFailureReason  string    `json:"last_failure_reason,omitempty"`
	TotalReloadCount   int       `json:"total_reload_count"`
	TotalFailureCount  int       `json:"total_failure_count"`
}

// AuditReadOpts filters the audit log at read time.
type AuditReadOpts struct {
	Since time.Time
	Limit int
}

// AuditDir returns the directory where iris keeps its state. Honors
// IRIS_AUDIT_DIR for tests; falls back to $HOME/.iris.
func AuditDir() (string, error) {
	if dir := os.Getenv(AuditDirEnv); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("audit: home dir: %w", err)
	}
	return filepath.Join(home, ".iris"), nil
}

// AuditPath returns the absolute path to `reload-history.jsonl`.
func AuditPath() (string, error) {
	dir, err := AuditDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, AuditFilename), nil
}

// AppendAudit serializes the entry as JSON, opens the audit log
// append-only, and writes a single line. Failure to write is logged at
// WARN and swallowed: the audit log is best-effort. Reload MUST NOT crash
// the caller on audit failure.
func AppendAudit(entry AuditEntry) error {
	path, err := AuditPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("audit: mkdir: %w", err)
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if entry.Warnings == nil {
		entry.Warnings = []string{}
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit: marshal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit: open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("audit: write: %w", err)
	}
	return nil
}

// AppendAuditBestEffort wraps AppendAudit, logging failures and swallowing
// the error. Production callers use this so a broken audit log never
// blocks a reload.
func AppendAuditBestEffort(entry AuditEntry, log *slog.Logger) {
	if err := AppendAudit(entry); err != nil {
		if log == nil {
			log = slog.Default()
		}
		log.Warn("audit append failed", "err", err)
	}
}

// ReadAudit returns audit entries from the log, in append order, after
// applying since/limit filters. A missing log returns an empty slice and
// nil error.
func ReadAudit(opts AuditReadOpts) ([]AuditEntry, error) {
	path, err := AuditPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("audit: open %s: %w", path, err)
	}
	defer f.Close()

	var entries []AuditEntry
	scanner := bufio.NewScanner(f)
	// Audit lines carry build output; raise the buffer ceiling so a large
	// build_output doesn't surface a bufio "token too long" error.
	const maxLine = 4 * 1024 * 1024 // 4 MiB
	scanner.Buffer(make([]byte, 64*1024), maxLine)
	for scanner.Scan() {
		var e AuditEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			// Skip malformed lines; never crash a reader because the writer
			// was upgraded mid-flight.
			continue
		}
		if !opts.Since.IsZero() && e.Timestamp.Before(opts.Since) {
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return entries, fmt.Errorf("audit: scan: %w", err)
	}

	if opts.Limit > 0 && len(entries) > opts.Limit {
		entries = entries[len(entries)-opts.Limit:]
	}
	return entries, nil
}

// AggregateBySourceRepo dedupes entries by TargetSourceRepo, computes
// counts, projects the most-recent entry's fields, and sorts descending
// by LastReloadAt.
func AggregateBySourceRepo(entries []AuditEntry) []AggregateEntry {
	if len(entries) == 0 {
		return nil
	}
	byRepo := make(map[string]*AggregateEntry, len(entries))
	for _, e := range entries {
		agg, ok := byRepo[e.TargetSourceRepo]
		if !ok {
			agg = &AggregateEntry{SourceRepo: e.TargetSourceRepo}
			byRepo[e.TargetSourceRepo] = agg
		}
		agg.TotalReloadCount++
		if e.Outcome != "success" {
			agg.TotalFailureCount++
		}
		// Project most-recent fields by Timestamp; we'll see entries in
		// append order, but the audit log may have been edited.
		if e.Timestamp.After(agg.LastReloadAt) {
			agg.LastReloadAt = e.Timestamp
			agg.LastOutcome = e.Outcome
			agg.LastMode = e.Mode
			agg.LastPrePullSha = e.PrePullSha
			agg.LastPostPullSha = e.PostPullSha
			agg.LastFailureReason = e.FailureReason
		}
	}
	out := make([]AggregateEntry, 0, len(byRepo))
	for _, agg := range byRepo {
		out = append(out, *agg)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastReloadAt.After(out[j].LastReloadAt)
	})
	return out
}

// LatestForRepo scans the audit log and returns the most-recent entry whose
// TargetSourceRepo equals sourceRepo. Returns nil if no entry exists.
func LatestForRepo(sourceRepo string) (*AuditEntry, error) {
	entries, err := ReadAudit(AuditReadOpts{})
	if err != nil {
		return nil, err
	}
	var latest *AuditEntry
	for i := range entries {
		e := entries[i]
		if e.TargetSourceRepo != sourceRepo {
			continue
		}
		if latest == nil || e.Timestamp.After(latest.Timestamp) {
			tmp := e
			latest = &tmp
		}
	}
	return latest, nil
}
