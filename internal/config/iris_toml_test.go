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

func TestLoadIrisToml_MissingFile(t *testing.T) {
	doc, errs, err := LoadIrisToml(filepath.Join(t.TempDir(), "nope.toml"), true)
	if err != nil {
		t.Fatalf("io error: %v", err)
	}
	if doc != nil {
		t.Fatalf("expected nil doc when file missing")
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "file not found") {
		t.Fatalf("expected file-not-found error, got: %v", errs)
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
