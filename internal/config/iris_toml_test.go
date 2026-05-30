package config

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadIrisToml_HappyPathSelf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".iris.toml")
	writeFile(t, path, `
schema_version = 1
default_branch = "main"

[build]
command = ["make", "build"]

[restart]
mechanism = "exit_code"
`)
	doc, errs, err := LoadIrisToml(path, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors, got: %v", errs)
	}
	if doc.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d", doc.SchemaVersion)
	}
	if doc.DefaultBranch != "main" {
		t.Fatalf("default_branch = %q", doc.DefaultBranch)
	}
	if len(doc.Build.Command) != 2 || doc.Build.Command[0] != "make" {
		t.Fatalf("build.command = %#v", doc.Build.Command)
	}
	if doc.Restart.Mechanism != MechanismExitCode {
		t.Fatalf("restart.mechanism = %q", doc.Restart.Mechanism)
	}
	if doc.Restart.ResolvedExitCode() != 75 {
		t.Fatalf("default exit code should be 75, got %d", doc.Restart.ResolvedExitCode())
	}
}

// TestLoadIrisToml_MissingFileIsSilent verifies the consumer-ergonomics
// contract: a missing `.iris.toml` is a non-event, not a validation
// error. Callers that require a config check `doc == nil` themselves
// and synthesize their own error.
func TestLoadIrisToml_MissingFileIsSilent(t *testing.T) {
	doc, errs, err := LoadIrisToml(filepath.Join(t.TempDir(), "nope.toml"), true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if doc != nil {
		t.Fatalf("expected nil doc when file missing, got %+v", doc)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors when file missing, got: %v", errs)
	}
}

func TestLoadIrisToml_MalformedReportsLineNumber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".iris.toml")
	writeFile(t, path, "schema_version = 1\nbroken = [unclosed\n")
	_, errs, err := LoadIrisToml(path, true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected parse error")
	}
	if errs[0].Line == 0 {
		t.Fatalf("expected line number in parse error: %+v", errs[0])
	}
}

func TestValidate_MissingSchemaVersion(t *testing.T) {
	doc := &IrisToml{
		Build:   BuildBlock{Command: []string{"make"}},
		Restart: RestartBlock{Mechanism: MechanismExitCode},
	}
	errs := doc.Validate(true)
	if len(errs) == 0 {
		t.Fatal("expected error for missing schema_version")
	}
	if errs[0].Field != "schema_version" {
		t.Fatalf("expected field schema_version, got %q", errs[0].Field)
	}
}

func TestValidate_UnknownSchemaVersion(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 99,
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismExitCode},
	}
	errs := doc.Validate(true)
	if len(errs) == 0 {
		t.Fatal("expected error for unknown schema_version")
	}
	if !strings.Contains(errs[0].Message, "99") {
		t.Fatalf("expected error to name 99, got: %v", errs[0])
	}
}

func TestValidate_MissingBuild(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		Restart:       RestartBlock{Mechanism: MechanismExitCode},
	}
	errs := doc.Validate(true)
	found := false
	for _, e := range errs {
		if e.Field == "build.command" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected build.command missing error: %v", errs)
	}
}

func TestValidate_MissingRestart(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		Build:         BuildBlock{Command: []string{"make"}},
	}
	errs := doc.Validate(true)
	found := false
	for _, e := range errs {
		if e.Field == "restart.mechanism" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected restart.mechanism missing error: %v", errs)
	}
}

func TestValidate_MechanismFieldConflict_LaunchAgentWithPidFile(t *testing.T) {
	data := `
schema_version = 1
[build]
command = ["make"]
[restart]
mechanism = "launchagent"
label = "com.example.x"
pid_file = "/tmp/foo.pid"
`
	doc, errs, err := DecodeIrisToml([]byte(data), "stub.toml", false)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = doc
	found := false
	for _, e := range errs {
		if e.Field == "restart.pid_file" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected pid_file conflict error: %v", errs)
	}
}

func TestValidate_ExitCodeOnCrossTarget(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismExitCode},
	}
	errs := doc.Validate(false) // isSelf=false
	found := false
	for _, e := range errs {
		if e.Field == "restart.mechanism" && strings.Contains(e.Message, "self-only") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected exit_code-on-cross error: %v", errs)
	}
}

func TestValidate_ZeroExitCode(t *testing.T) {
	zero := 0
	doc := &IrisToml{
		SchemaVersion: 1,
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismExitCode, Code: &zero},
	}
	errs := doc.Validate(true)
	found := false
	for _, e := range errs {
		if e.Field == "restart.code" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected zero-exit-code error: %v", errs)
	}
}

func TestValidate_LaunchAgentMissingLabel(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismLaunchAgent},
	}
	errs := doc.Validate(false)
	found := false
	for _, e := range errs {
		if e.Field == "restart.label" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing-label error: %v", errs)
	}
}

func TestValidate_SignalMissingPidFile(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismSignal, Signal: "SIGTERM"},
	}
	errs := doc.Validate(false)
	found := false
	for _, e := range errs {
		if e.Field == "restart.pid_file" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing-pid_file error: %v", errs)
	}
}

func TestValidate_SignalUnknownName(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismSignal, Signal: "SIGFAKE", PidFile: "/tmp/x.pid"},
	}
	errs := doc.Validate(false)
	found := false
	for _, e := range errs {
		if e.Field == "restart.signal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unknown-signal error: %v", errs)
	}
}

func TestValidate_ExecMissingCommand(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismExec},
	}
	errs := doc.Validate(false)
	found := false
	for _, e := range errs {
		if e.Field == "restart.command" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing-command error: %v", errs)
	}
}

func TestValidate_NoneMechanismIsOk(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismNone},
	}
	errs := doc.Validate(false)
	if len(errs) != 0 {
		t.Fatalf("expected no errors for mechanism=none, got: %v", errs)
	}
}

func TestValidate_UnknownTopLevelField(t *testing.T) {
	data := `
schema_version = 1
bogus_field = "x"
[build]
command = ["make"]
[restart]
mechanism = "none"
`
	_, errs, _ := DecodeIrisToml([]byte(data), "stub.toml", false)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Field, "bogus_field") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unknown-field error: %v", errs)
	}
}

func TestValidate_AbsoluteWorkingDirectoryRejected(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		Build:         BuildBlock{Command: []string{"make"}, WorkingDirectory: "/etc"},
		Restart:       RestartBlock{Mechanism: MechanismNone},
	}
	errs := doc.Validate(false)
	found := false
	for _, e := range errs {
		if e.Field == "build.working_directory" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected absolute-working_directory error: %v", errs)
	}
}

// TestValidate_SelfMustUseExitCode verifies the spec scenario "Refuses
// non-exit_code mechanism for self-reload" — iris's own .iris.toml must
// declare mechanism=exit_code; anything else would suicide the daemon
// mid-handler.
func TestValidate_SelfMustUseExitCode(t *testing.T) {
	cases := []RestartMechanism{
		MechanismLaunchAgent,
		MechanismLaunchDaemon,
		MechanismSignal,
		MechanismExec,
		MechanismNone,
	}
	for _, m := range cases {
		t.Run(string(m), func(t *testing.T) {
			r := RestartBlock{Mechanism: m}
			switch m {
			case MechanismLaunchAgent, MechanismLaunchDaemon:
				r.Label = "com.example.x"
			case MechanismSignal:
				r.PidFile = "/tmp/x.pid"
				r.Signal = "SIGTERM"
			case MechanismExec:
				r.Command = []string{"true"}
			}
			doc := &IrisToml{
				SchemaVersion: 1,
				Build:         BuildBlock{Command: []string{"make"}},
				Restart:       r,
			}
			errs := doc.Validate(true) // isSelf=true
			found := false
			for _, e := range errs {
				if e.Field == "restart.mechanism" && strings.Contains(e.Message, "must use exit_code") {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected self-must-use-exit_code error for mechanism %q: %v", m, errs)
			}
		})
	}
}

// TestValidate_HookAbsoluteWorkingDirectoryRejected verifies the
// HookBlock.WorkingDirectory escape guard added in ralph-review loop 1.
// Mirrors the BuildBlock check; covers `[pre_flight]`, `[verify]`, and
// `[post_merge]`.
func TestValidate_HookAbsoluteWorkingDirectoryRejected(t *testing.T) {
	assign := func(doc *IrisToml, block string, hook *HookBlock) {
		switch block {
		case "pre_flight":
			doc.PreFlight = hook
		case "verify":
			doc.Verify = hook
		case "post_merge":
			doc.PostMerge = hook
		}
	}
	for _, block := range []string{"pre_flight", "verify", "post_merge"} {
		t.Run(block+"/absolute", func(t *testing.T) {
			hook := &HookBlock{Command: []string{"true"}, WorkingDirectory: "/etc"}
			doc := &IrisToml{
				SchemaVersion: 1,
				Build:         BuildBlock{Command: []string{"make"}},
				Restart:       RestartBlock{Mechanism: MechanismNone},
			}
			assign(doc, block, hook)
			errs := doc.Validate(false)
			found := false
			for _, e := range errs {
				if e.Field == block+".working_directory" && strings.Contains(e.Message, "relative") {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected %s.working_directory absolute error: %v", block, errs)
			}
		})
		t.Run(block+"/escape", func(t *testing.T) {
			hook := &HookBlock{Command: []string{"true"}, WorkingDirectory: "../escape"}
			doc := &IrisToml{
				SchemaVersion: 1,
				Build:         BuildBlock{Command: []string{"make"}},
				Restart:       RestartBlock{Mechanism: MechanismNone},
			}
			assign(doc, block, hook)
			errs := doc.Validate(false)
			found := false
			for _, e := range errs {
				if e.Field == block+".working_directory" && strings.Contains(e.Message, "escape") {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected %s.working_directory escape error: %v", block, errs)
			}
		})
	}
}

// TestValidate_PostMergeHappy verifies a `[post_merge]` block decodes
// cleanly and validates with no errors when shaped like the existing hook
// blocks. Sister test to the pre_flight/verify happy paths.
func TestValidate_PostMergeHappy(t *testing.T) {
	data := `
schema_version = 1

[build]
command = ["make", "build"]

[restart]
mechanism = "none"

[post_merge]
command = ["echo", "done"]
working_directory = "."
timeout_seconds = 5
`
	doc, errs, err := DecodeIrisToml([]byte(data), "stub.toml", false)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors, got: %v", errs)
	}
	if doc.PostMerge == nil {
		t.Fatal("expected PostMerge to be populated")
	}
	if got := doc.PostMerge.Command; len(got) != 2 || got[0] != "echo" {
		t.Fatalf("PostMerge.Command = %#v", got)
	}
	if doc.PostMerge.ResolvedTimeoutSeconds(60) != 5 {
		t.Fatalf("PostMerge resolved timeout = %d", doc.PostMerge.ResolvedTimeoutSeconds(60))
	}
}

// TestValidate_PostMergeMissingCommand verifies that an empty `[post_merge]`
// command surfaces the same validation error as the other hook blocks.
func TestValidate_PostMergeMissingCommand(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismNone},
		PostMerge:     &HookBlock{},
	}
	errs := doc.Validate(false)
	found := false
	for _, e := range errs {
		if e.Field == "post_merge.command" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected post_merge.command missing error: %v", errs)
	}
}

// --- dogfood_branch / ship_ci_timeout_seconds (iris-validate-config spec) ---

// Scenario: Missing dogfood_branch is valid.
func TestValidate_DogfoodBranchMissingIsValid(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		DefaultBranch: "main",
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismNone},
	}
	errs := doc.Validate(false)
	for _, e := range errs {
		if strings.Contains(e.Field, "dogfood") {
			t.Fatalf("unexpected dogfood error for unset field: %v", e)
		}
	}
}

// Scenario: Valid dogfood_branch passes and is reflected in the resolved doc.
func TestValidate_DogfoodBranchValid(t *testing.T) {
	data := `
schema_version = 1
default_branch = "main"
dogfood_branch = "dev"

[build]
command = ["make", "build"]

[restart]
mechanism = "none"
`
	doc, errs, err := DecodeIrisToml([]byte(data), "stub.toml", false)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors, got: %v", errs)
	}
	if doc.DogfoodBranch != "dev" {
		t.Fatalf("dogfood_branch = %q, want %q", doc.DogfoodBranch, "dev")
	}
}

// Scenario: Invalid branch name reports a remediation hint.
func TestValidate_DogfoodBranchInvalidName(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		DefaultBranch: "main",
		DogfoodBranch: "no spaces allowed",
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismNone},
	}
	errs := doc.Validate(false)
	var got *ValidationError
	for i := range errs {
		if errs[i].Field == "dogfood_branch" {
			got = &errs[i]
		}
	}
	if got == nil {
		t.Fatalf("expected dogfood_branch error, got: %v", errs)
	}
	if got.Message != "invalid git branch name" {
		t.Fatalf("message = %q, want %q", got.Message, "invalid git branch name")
	}
	if got.Hint == "" {
		t.Fatalf("expected a remediation hint, got empty")
	}
}

// Scenario: dogfood_branch equal to default_branch is invalid.
func TestValidate_DogfoodBranchEqualsDefaultBranch(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion: 1,
		DefaultBranch: "main",
		DogfoodBranch: "main",
		Build:         BuildBlock{Command: []string{"make"}},
		Restart:       RestartBlock{Mechanism: MechanismNone},
	}
	errs := doc.Validate(false)
	var got *ValidationError
	for i := range errs {
		if errs[i].Field == "dogfood_branch" {
			got = &errs[i]
		}
	}
	if got == nil {
		t.Fatalf("expected dogfood_branch error, got: %v", errs)
	}
	if !strings.Contains(got.Message, "default_branch") {
		t.Fatalf("message should mention default_branch, got %q", got.Message)
	}
	if got.Hint == "" {
		t.Fatalf("expected a hint recommending a distinct name")
	}
}

// Scenario: Negative ship_ci_timeout_seconds is invalid.
func TestValidate_NegativeShipCITimeout(t *testing.T) {
	doc := &IrisToml{
		SchemaVersion:        1,
		DefaultBranch:        "main",
		ShipCITimeoutSeconds: -1,
		Build:                BuildBlock{Command: []string{"make"}},
		Restart:              RestartBlock{Mechanism: MechanismNone},
	}
	errs := doc.Validate(false)
	var got *ValidationError
	for i := range errs {
		if errs[i].Field == "ship_ci_timeout_seconds" {
			got = &errs[i]
		}
	}
	if got == nil {
		t.Fatalf("expected ship_ci_timeout_seconds error, got: %v", errs)
	}
	if !strings.Contains(got.Message, "non-negative") {
		t.Fatalf("message should state the non-negativity rule, got %q", got.Message)
	}
}

// ship_ci_timeout_seconds defaults to 600 when unset, applied at resolution
// time (mirrors the build/exec timeout resolver pattern).
func TestResolvedShipCITimeout_DefaultsTo600(t *testing.T) {
	doc := &IrisToml{SchemaVersion: 1}
	if got := doc.ResolvedShipCITimeoutSeconds(); got != 600 {
		t.Fatalf("default ship_ci_timeout_seconds = %d, want 600", got)
	}
	doc.ShipCITimeoutSeconds = 30
	if got := doc.ResolvedShipCITimeoutSeconds(); got != 30 {
		t.Fatalf("explicit ship_ci_timeout_seconds = %d, want 30", got)
	}
}

func TestSignalByName_Aliases(t *testing.T) {
	cases := []struct {
		in   string
		want syscall.Signal
	}{
		{"SIGTERM", syscall.SIGTERM},
		{"sigterm", syscall.SIGTERM},
		{"TERM", syscall.SIGTERM},
		{"HUP", syscall.SIGHUP},
		{"SIGUSR2", syscall.SIGUSR2},
	}
	for _, c := range cases {
		got, ok := SignalByName(c.in)
		if !ok || got != c.want {
			t.Errorf("SignalByName(%q) = %v,%v want %v,true", c.in, got, ok, c.want)
		}
	}
}
