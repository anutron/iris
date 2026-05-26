package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// IrisTomlFilename is the conventional filename for the daemon-self-management
// declaration. It lives at the source repo root.
const IrisTomlFilename = ".iris.toml"

// SupportedSchemaVersion is the only schema_version iris accepts today.
const SupportedSchemaVersion = 1

// DefaultExitCode is the non-zero exit code used by the `exit_code` self-reload
// mechanism when the project does not override it. 75 is EX_TEMPFAIL from
// sysexits.h; iris's own non-reload codepaths use 0 (clean) or 1 (startup).
const DefaultExitCode = 75

// DefaultBuildTimeoutSeconds is the default for `[build] timeout_seconds`.
const DefaultBuildTimeoutSeconds = 600

// DefaultHookTimeoutSeconds is the default for `[pre_flight]` and `[verify]`
// `timeout_seconds`.
const DefaultHookTimeoutSeconds = 60

// DefaultExecTimeoutSeconds is the default for the `exec` restart mechanism's
// `timeout_seconds`.
const DefaultExecTimeoutSeconds = 30

// DefaultVerifyTimeoutSeconds is the default for the `[verify]` hook.
const DefaultVerifyTimeoutSeconds = 30

// RestartMechanism enumerates the supported `[restart] mechanism` values.
type RestartMechanism string

const (
	MechanismExitCode     RestartMechanism = "exit_code"
	MechanismLaunchAgent  RestartMechanism = "launchagent"
	MechanismLaunchDaemon RestartMechanism = "launchdaemon"
	MechanismSignal       RestartMechanism = "signal"
	MechanismExec         RestartMechanism = "exec"
	MechanismNone         RestartMechanism = "none"
)

// IrisToml is the parsed `.iris.toml` document.
type IrisToml struct {
	SchemaVersion int           `toml:"schema_version"`
	DefaultBranch string        `toml:"default_branch"`
	Build         BuildBlock    `toml:"build"`
	Restart       RestartBlock  `toml:"restart"`
	PreFlight    *HookBlock    `toml:"pre_flight"`
	Verify       *HookBlock    `toml:"verify"`
}

// BuildBlock declares the build step.
type BuildBlock struct {
	Command          []string          `toml:"command"`
	TimeoutSeconds   int               `toml:"timeout_seconds"`
	WorkingDirectory string            `toml:"working_directory"`
	Env              map[string]string `toml:"env"`
}

// RestartBlock declares how to bring the new binary into service.
//
// Fields are intentionally a flat union so the TOML parser can detect
// cross-mechanism field mismatches: iris validates that only the fields
// belonging to the declared mechanism are set.
type RestartBlock struct {
	Mechanism      RestartMechanism `toml:"mechanism"`
	Code           *int             `toml:"code"`
	Label          string           `toml:"label"`
	PidFile        string           `toml:"pid_file"`
	Signal         string           `toml:"signal"`
	Command        []string         `toml:"command"`
	TimeoutSeconds int              `toml:"timeout_seconds"`
}

// HookBlock is shared by `[pre_flight]` and `[verify]`.
type HookBlock struct {
	Command          []string `toml:"command"`
	TimeoutSeconds   int      `toml:"timeout_seconds"`
	WorkingDirectory string   `toml:"working_directory"`
}

// ValidationError describes a single failure surfaced by parse or
// cross-validation of a `.iris.toml` file.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Line    int    `json:"line,omitempty"`
}

// Error implements the error interface so a single ValidationError can be
// returned from helpers that only ever produce one.
func (e ValidationError) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Field, e.Message, e.Hint)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// LoadIrisToml reads and parses the `.iris.toml` at the given path.
//
// Returns:
//   - (*IrisToml, nil, nil) on a successful parse with no validation errors
//   - (*IrisToml, errs, nil) on a successful parse with cross-validation errors
//   - (nil, errs, nil) when the file is missing or malformed (errs name the issue)
//   - (nil, nil, err) for I/O or unexpected errors only
//
// Validation requires knowing whether the file is iris's own (`isSelf=true`):
// the `exit_code` restart mechanism is only legal for the self-managed daemon.
// Call ValidateConfig later if you do not know isSelf at load time.
func LoadIrisToml(path string, isSelf bool) (*IrisToml, []ValidationError, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, []ValidationError{{
				Field:   IrisTomlFilename,
				Message: fmt.Sprintf("file not found at %s", path),
				Hint:    "create .iris.toml at the source repo root",
			}}, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	return DecodeIrisToml(data, path, isSelf)
}

// DecodeIrisToml parses raw bytes into an IrisToml. Convenience for callers
// that already have the bytes (tests).
func DecodeIrisToml(data []byte, sourcePath string, isSelf bool) (*IrisToml, []ValidationError, error) {
	var doc IrisToml
	meta, err := toml.Decode(string(data), &doc)
	if err != nil {
		ve := ValidationError{
			Field:   IrisTomlFilename,
			Message: fmt.Sprintf("TOML parse error: %v", err),
			Hint:    "fix the TOML syntax",
		}
		if line := tomlErrorLine(err); line > 0 {
			ve.Line = line
		}
		return nil, []ValidationError{ve}, nil
	}

	var errs []ValidationError
	for _, key := range meta.Undecoded() {
		errs = append(errs, ValidationError{
			Field:   key.String(),
			Message: "unknown field",
			Hint:    "remove the field or check for a typo against the .iris.toml schema",
		})
	}

	errs = append(errs, doc.Validate(isSelf)...)
	return &doc, errs, nil
}

// tomlErrorLine extracts a line number from a github.com/BurntSushi/toml
// parser error if one is present.
func tomlErrorLine(err error) int {
	var pe toml.ParseError
	if errors.As(err, &pe) {
		return pe.Position.Line
	}
	return 0
}

// Validate cross-validates the document against the v1 schema. isSelf marks
// whether the document belongs to iris's own deployed source repo; this
// controls whether `exit_code` is a legal restart mechanism.
func (c *IrisToml) Validate(isSelf bool) []ValidationError {
	var errs []ValidationError

	switch {
	case c.SchemaVersion == 0:
		errs = append(errs, ValidationError{
			Field:   "schema_version",
			Message: "missing required field",
			Hint:    fmt.Sprintf("set schema_version = %d", SupportedSchemaVersion),
		})
	case c.SchemaVersion != SupportedSchemaVersion:
		errs = append(errs, ValidationError{
			Field:   "schema_version",
			Message: fmt.Sprintf("unsupported schema_version %d", c.SchemaVersion),
			Hint:    fmt.Sprintf("iris supports schema_version = %d", SupportedSchemaVersion),
		})
	}

	errs = append(errs, c.Build.validate()...)
	errs = append(errs, c.Restart.validate(isSelf)...)
	if c.PreFlight != nil {
		errs = append(errs, c.PreFlight.validate("pre_flight")...)
	}
	if c.Verify != nil {
		errs = append(errs, c.Verify.validate("verify")...)
	}

	return errs
}

func (b *BuildBlock) validate() []ValidationError {
	var errs []ValidationError
	if len(b.Command) == 0 {
		errs = append(errs, ValidationError{
			Field:   "build.command",
			Message: "missing required field",
			Hint:    `set [build] command = ["make", "build"] (argv, no shell)`,
		})
	}
	if b.TimeoutSeconds < 0 {
		errs = append(errs, ValidationError{
			Field:   "build.timeout_seconds",
			Message: "must be non-negative",
		})
	}
	if b.WorkingDirectory != "" && filepath.IsAbs(b.WorkingDirectory) {
		errs = append(errs, ValidationError{
			Field:   "build.working_directory",
			Message: "must be relative to the source repo root",
			Hint:    "use a path like \".\" or \"subdir\", not an absolute path",
		})
	}
	if b.WorkingDirectory != "" {
		clean := filepath.Clean(b.WorkingDirectory)
		if clean == ".." || strings.HasPrefix(clean, "../") {
			errs = append(errs, ValidationError{
				Field:   "build.working_directory",
				Message: "must not escape the source repo root",
			})
		}
	}
	return errs
}

func (r *RestartBlock) validate(isSelf bool) []ValidationError {
	var errs []ValidationError
	if r.Mechanism == "" {
		errs = append(errs, ValidationError{
			Field:   "restart.mechanism",
			Message: "missing required field",
			Hint:    "set [restart] mechanism = \"exit_code\" | \"launchagent\" | \"launchdaemon\" | \"signal\" | \"exec\" | \"none\"",
		})
		return errs
	}

	allowed := map[RestartMechanism]struct{}{
		MechanismExitCode:     {},
		MechanismLaunchAgent:  {},
		MechanismLaunchDaemon: {},
		MechanismSignal:       {},
		MechanismExec:         {},
		MechanismNone:         {},
	}
	if _, ok := allowed[r.Mechanism]; !ok {
		errs = append(errs, ValidationError{
			Field:   "restart.mechanism",
			Message: fmt.Sprintf("unknown mechanism %q", r.Mechanism),
			Hint:    "use one of: exit_code, launchagent, launchdaemon, signal, exec, none",
		})
		return errs
	}

	// Cross-mechanism field exclusivity. Each mechanism owns a set of
	// fields; fields belonging to a different mechanism MUST NOT be set.
	required, allowed_fields, hint := mechanismFields(r.Mechanism)

	type presence struct {
		field string
		set   bool
	}
	presents := []presence{
		{"code", r.Code != nil},
		{"label", r.Label != ""},
		{"pid_file", r.PidFile != ""},
		{"signal", r.Signal != ""},
		{"command", len(r.Command) > 0},
		{"timeout_seconds", r.TimeoutSeconds != 0},
	}
	for _, p := range presents {
		if !p.set {
			continue
		}
		if _, ok := allowed_fields[p.field]; !ok {
			errs = append(errs, ValidationError{
				Field:   "restart." + p.field,
				Message: fmt.Sprintf("field does not belong to mechanism %q", r.Mechanism),
				Hint:    hint,
			})
		}
	}
	for f := range required {
		ok := false
		for _, p := range presents {
			if p.field == f && p.set {
				ok = true
				break
			}
		}
		if !ok {
			errs = append(errs, ValidationError{
				Field:   "restart." + f,
				Message: fmt.Sprintf("missing required field for mechanism %q", r.Mechanism),
				Hint:    hint,
			})
		}
	}

	switch r.Mechanism {
	case MechanismExitCode:
		if !isSelf {
			errs = append(errs, ValidationError{
				Field:   "restart.mechanism",
				Message: "exit_code is a self-only mechanism (target source repo is not iris's own)",
				Hint:    "use launchagent, signal, exec, or another mechanism for cross-reload",
			})
		}
		if r.Code != nil && *r.Code == 0 {
			errs = append(errs, ValidationError{
				Field:   "restart.code",
				Message: "code must be non-zero (LaunchAgent's KeepAlive SuccessfulExit=false requires non-zero exit to respawn)",
				Hint:    "omit the field to use the default of 75 (EX_TEMPFAIL)",
			})
		}
	case MechanismSignal:
		if r.Signal != "" {
			if _, ok := SignalByName(r.Signal); !ok {
				errs = append(errs, ValidationError{
					Field:   "restart.signal",
					Message: fmt.Sprintf("unknown signal name %q", r.Signal),
					Hint:    "use a standard name like SIGTERM, SIGHUP, SIGUSR1, SIGUSR2",
				})
			}
		}
	}

	return errs
}

// mechanismFields returns the (required, allowed) sets for a given mechanism
// plus a remediation hint describing the legal shape.
func mechanismFields(m RestartMechanism) (required, allowed map[string]struct{}, hint string) {
	required = map[string]struct{}{}
	allowed = map[string]struct{}{}
	switch m {
	case MechanismExitCode:
		allowed = map[string]struct{}{"code": {}}
		hint = `mechanism = "exit_code" accepts optional code = <non-zero int>`
	case MechanismLaunchAgent, MechanismLaunchDaemon:
		required = map[string]struct{}{"label": {}}
		allowed = map[string]struct{}{"label": {}}
		hint = `mechanism = "` + string(m) + `" requires label = "com.example.label"`
	case MechanismSignal:
		required = map[string]struct{}{"pid_file": {}, "signal": {}}
		allowed = map[string]struct{}{"pid_file": {}, "signal": {}}
		hint = `mechanism = "signal" requires pid_file = "..." and signal = "SIGTERM"`
	case MechanismExec:
		required = map[string]struct{}{"command": {}}
		allowed = map[string]struct{}{"command": {}, "timeout_seconds": {}}
		hint = `mechanism = "exec" requires command = ["argv", ...] and accepts optional timeout_seconds`
	case MechanismNone:
		hint = `mechanism = "none" accepts no other fields`
	}
	return
}

func (h *HookBlock) validate(blockName string) []ValidationError {
	var errs []ValidationError
	if len(h.Command) == 0 {
		errs = append(errs, ValidationError{
			Field:   blockName + ".command",
			Message: "missing required field",
			Hint:    fmt.Sprintf(`set [%s] command = ["argv", ...]`, blockName),
		})
	}
	if h.TimeoutSeconds < 0 {
		errs = append(errs, ValidationError{
			Field:   blockName + ".timeout_seconds",
			Message: "must be non-negative",
		})
	}
	return errs
}

// ResolvedExitCode returns the configured exit code or the default if unset.
func (r RestartBlock) ResolvedExitCode() int {
	if r.Code == nil {
		return DefaultExitCode
	}
	return *r.Code
}

// ResolvedBuildTimeout returns the configured build timeout or the default
// if unset.
func (b BuildBlock) ResolvedTimeoutSeconds() int {
	if b.TimeoutSeconds == 0 {
		return DefaultBuildTimeoutSeconds
	}
	return b.TimeoutSeconds
}

// ResolvedWorkingDirectory returns the configured working_directory or ".".
func (b BuildBlock) ResolvedWorkingDirectory() string {
	if b.WorkingDirectory == "" {
		return "."
	}
	return b.WorkingDirectory
}

// ResolvedTimeoutSeconds for a HookBlock; defaults to 60.
func (h HookBlock) ResolvedTimeoutSeconds(defaultSec int) int {
	if h.TimeoutSeconds == 0 {
		return defaultSec
	}
	return h.TimeoutSeconds
}

// ResolvedExecTimeoutSeconds for the exec mechanism; defaults to 30.
func (r RestartBlock) ResolvedExecTimeoutSeconds() int {
	if r.TimeoutSeconds == 0 {
		return DefaultExecTimeoutSeconds
	}
	return r.TimeoutSeconds
}
