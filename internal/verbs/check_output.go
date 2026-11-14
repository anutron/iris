package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxCheckOutputBytes = 64 << 10
	maxFailureBytes     = 16 << 10
)

// CheckSummary is best-effort RSpec reporting; ExitCode remains authoritative.
type CheckSummary struct {
	Examples int `json:"examples"`
	Failures int `json:"failures"`
	Pending  int `json:"pending"`
}

var rspecSummary = regexp.MustCompile(`(?m)^([0-9]+) examples?, ([0-9]+) failures?(?:, ([0-9]+) pending)?[ \t]*\r?$`)
var ansiColor = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// boundCheckOutput keeps small-result wire formats unchanged. Large logs are
// preserved before their in-memory output is replaced, including raw invalid
// UTF-8 bytes that JSON cannot represent losslessly.
func boundCheckOutput(result *RunChecksResult) error {
	if len(result.Output) <= MaxCheckOutputBytes {
		return nil
	}
	full := result.Output
	result.OutputTruncated = true
	result.OutputBytes = len(full)
	tail := full[len(full)-MaxCheckOutputBytes:]
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	result.Output = strings.ToValidUTF8(tail, "?")
	parseable := strings.ReplaceAll(ansiColor.ReplaceAllString(full, ""), "\r\n", "\n")
	if matches := rspecSummary.FindAllStringSubmatch(parseable, -1); len(matches) > 0 {
		m := matches[len(matches)-1]
		examples, _ := strconv.Atoi(m[1])
		failures, _ := strconv.Atoi(m[2])
		pending, _ := strconv.Atoi(m[3])
		result.Summary = &CheckSummary{Examples: examples, Failures: failures, Pending: pending}
	}
	if start := strings.Index("\n"+parseable, "\nFailures:\n"); start >= 0 {
		section := parseable[start:]
		if end := strings.Index(section, "\nFinished in "); end >= 0 {
			section = section[:end]
		}
		if len(section) > maxFailureBytes {
			section = section[:maxFailureBytes]
			for len(section) > 0 && !utf8.RuneStart(parseable[start+len(section)]) {
				section = section[:len(section)-1]
			}
			result.FailureSectionTruncated = true
		}
		result.FailureSection = strings.ToValidUTF8(section, "?")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("preserve full check log: %w", err)
	}
	dir := filepath.Join(cache, "iris", "check-logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("preserve full check log: %w", err)
	}
	f, err := os.CreateTemp(dir, "check-*.log")
	if err != nil {
		return fmt.Errorf("preserve full check log: %w", err)
	}
	_, writeErr := f.WriteString(full)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(f.Name())
		return fmt.Errorf("preserve full check log: write=%v close=%v", writeErr, closeErr)
	}
	result.LogPath = f.Name()
	return nil
}
