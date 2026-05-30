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

// DefaultShipCITimeoutSeconds is the default for `ship_ci_timeout_seconds` —
// how long iris:ship_feature (pr-auto) waits for CI checks before giving up.
const DefaultShipCITimeoutSeconds = 600

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
//
// Every top-level field carries a `kind` struct tag classifying it as
// either "shared" (project-wide; lives in `.iris.toml`) or "local"
// (per-developer; lives in `.iris.local.toml`). The FieldKind helper in
// iris_toml_taxonomy.go reads these tags; a companion test asserts every
// field has a valid classification so adding a new field without one
// fails CI.
type IrisToml struct {
	SchemaVersion        int          `toml:"schema_version"          json:"schema_version"                  kind:"shared"`
	DefaultBranch        string       `toml:"default_branch"          json:"default_branch,omitempty"        kind:"shared"`
	DogfoodBranch        string       `toml:"dogfood_branch"          json:"dogfood_branch,omitempty"        kind:"local"`
	ShipCITimeoutSeconds int          `toml:"ship_ci_timeout_seconds" json:"ship_ci_timeout_seconds,omitempty" kind:"local"`
	Build                BuildBlock   `toml:"build"                   json:"build"                           kind:"shared"`
	Restart              RestartBlock `toml:"restart"                 json:"restart"                         kind:"shared"`
	PreFlight            *HookBlock   `toml:"pre_flight"              json:"pre_flight,omitempty"            kind:"shared"`
	Verify               *HookBlock   `toml:"verify"                  json:"verify,omitempty"                kind:"shared"`
	PostMerge            *HookBlock   `toml:"post_merge"              json:"post_merge,omitempty"            kind:"shared"`
}

// BuildBlock declares the build step.
type BuildBlock struct {
	Command          []string          `toml:"command"           json:"command"`
	TimeoutSeconds   int               `toml:"timeout_seconds"   json:"timeout_seconds,omitempty"`
	WorkingDirectory string            `toml:"working_directory" json:"working_directory,omitempty"`
	Env              map[string]string `toml:"env"               json:"env,omitempty"`
}

// RestartBlock declares how to bring the new binary into service.
//
// Fields are intentionally a flat union so the TOML parser can detect
// cross-mechanism field mismatches: iris validates that only the fields
// belonging to the declared mechanism are set.
type RestartBlock struct {
	Mechanism      RestartMechanism `toml:"mechanism"       json:"mechanism"`
	Code           *int             `toml:"code"            json:"code,omitempty"`
	Label          string           `toml:"label"           json:"label,omitempty"`
	PidFile        string           `toml:"pid_file"        json:"pid_file,omitempty"`
	Signal         string           `toml:"signal"          json:"signal,omitempty"`
	Command        []string         `toml:"command"         json:"command,omitempty"`
	TimeoutSeconds int              `toml:"timeout_seconds" json:"timeout_seconds,omitempty"`
}

// HookBlock is shared by `[pre_flight]` and `[verify]`.
type HookBlock struct {
	Command          []string `toml:"command"           json:"command"`
	TimeoutSeconds   int      `toml:"timeout_seconds"   json:"timeout_seconds,omitempty"`
	WorkingDirectory string   `toml:"working_directory" json:"working_directory,omitempty"`
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
//   - (nil, errs, nil) when the file is malformed (errs name the issue)
//   - (nil, nil, nil) when the file is missing (ENOENT). Missing is NOT
//     an error: callers that require a config check for `doc == nil` and
//     synthesize their own error message. Callers that treat the config
//     as optional (e.g. iris:status) silently fall back to the
//     no-config code path.
//   - (nil, nil, err) for I/O or unexpected errors only
//
// Validation requires knowing whether the file is iris's own (`isSelf=true`):
// the `exit_code` restart mechanism is only legal for the self-managed daemon.
// Call ValidateConfig later if you do not know isSelf at load time.
func LoadIrisToml(path string, isSelf bool) (*IrisToml, []ValidationError, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
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

	errs = append(errs, c.validateDogfood()...)
	errs = append(errs, c.Build.validate()...)
	errs = append(errs, c.Restart.validate(isSelf)...)
	if c.PreFlight != nil {
		errs = append(errs, c.PreFlight.validate("pre_flight")...)
	}
	if c.Verify != nil {
		errs = append(errs, c.Verify.validate("verify")...)
	}
	if c.PostMerge != nil {
		errs = append(errs, c.PostMerge.validate("post_merge")...)
	}

	return errs
}

// validateDogfood cross-validates the opt-in dogfood/ship fields.
//
//   - dogfood_branch, when non-empty, MUST be a syntactically valid git branch
//     name (per `git check-ref-format --branch`, mirrored in pure Go by
//     validGitBranchName) and MUST NOT equal default_branch — the origin-first
//     model keeps local main read-only, so the dogfood branch needs a distinct
//     name to reset.
//   - ship_ci_timeout_seconds, when set, MUST be non-negative. Unset (0)
//     resolves to DefaultShipCITimeoutSeconds at use time.
func (c *IrisToml) validateDogfood() []ValidationError {
	var errs []ValidationError

	if c.DogfoodBranch != "" {
		switch {
		case !ValidGitBranchName(c.DogfoodBranch):
			errs = append(errs, ValidationError{
				Field:   "dogfood_branch",
				Message: "invalid git branch name",
				Hint:    "use a single ref name without spaces or invalid characters",
			})
		case c.DefaultBranch != "" && c.DogfoodBranch == c.DefaultBranch:
			errs = append(errs, ValidationError{
				Field:   "dogfood_branch",
				Message: "must not equal default_branch",
				Hint:    `choose a distinct branch name like "dev"; the origin-first model keeps the default branch read-only`,
			})
		}
	}

	if c.ShipCITimeoutSeconds < 0 {
		errs = append(errs, ValidationError{
			Field:   "ship_ci_timeout_seconds",
			Message: "must be non-negative",
			Hint:    "use a non-negative number of seconds, or omit the field to use the default of 600",
		})
	}

	return errs
}

// ValidGitBranchName reports whether name is a syntactically valid git branch
// name. It mirrors the rules `git check-ref-format --branch <name>` enforces,
// implemented in pure Go so the cross-validator stays free of a git dependency
// (the branch_create verb shells out to the real git for the authoritative
// check at mutation time). Keep the two in agreement.
//
// Exported so other packages (notably the set_local_config verb) can reuse
// the same rule without duplicating it.
func ValidGitBranchName(name string) bool {
	if name == "" || name == "@" {
		return false
	}
	// `--branch` mode rejects names beginning with a dash (would be parsed as
	// a flag), and refnames may not start or end with "/" or contain "//".
	if strings.HasPrefix(name, "-") {
		return false
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//") {
		return false
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") {
		return false
	}
	if strings.Contains(name, "..") || strings.Contains(name, "@{") {
		return false
	}
	for _, r := range name {
		// ASCII control characters and DEL are never allowed.
		if r < 0x20 || r == 0x7f {
			return false
		}
		switch r {
		case ' ', '~', '^', ':', '?', '*', '[', '\\':
			return false
		}
	}
	// No slash-separated component may begin with "." or end with ".lock".
	for _, comp := range strings.Split(name, "/") {
		if comp == "" || strings.HasPrefix(comp, ".") || strings.HasSuffix(comp, ".lock") {
			return false
		}
	}
	return true
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
	required, allowedFields, hint := mechanismFields(r.Mechanism)

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
		if _, ok := allowedFields[p.field]; !ok {
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
	default:
		// Self-managed daemons (iris itself) MUST use exit_code. Any other
		// mechanism would attempt to restart iris mid-handler (launchctl
		// kickstart on iris's own label, kill -SIGTERM on iris's own pid,
		// arbitrary exec spawning a launcher that may racefully replace iris).
		// The response-flush + lock-release + delayed-exit choreography in
		// reload.go's self path is only correct for exit_code. Reject anything
		// else at parse time so the operator sees the error before the verb
		// runs.
		if isSelf {
			errs = append(errs, ValidationError{
				Field:   "restart.mechanism",
				Message: fmt.Sprintf("self-managed daemons must use exit_code (got %q)", r.Mechanism),
				Hint:    `set mechanism = "exit_code"; the LaunchAgent KeepAlive respawns iris from the new binary`,
			})
		}
	}
	switch r.Mechanism {
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
	if h.WorkingDirectory != "" && filepath.IsAbs(h.WorkingDirectory) {
		errs = append(errs, ValidationError{
			Field:   blockName + ".working_directory",
			Message: "must be relative to the source repo root",
			Hint:    "use a path like \".\" or \"subdir\", not an absolute path",
		})
	}
	if h.WorkingDirectory != "" {
		clean := filepath.Clean(h.WorkingDirectory)
		if clean == ".." || strings.HasPrefix(clean, "../") {
			errs = append(errs, ValidationError{
				Field:   blockName + ".working_directory",
				Message: "must not escape the source repo root",
			})
		}
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

// ResolvedShipCITimeoutSeconds returns the configured ship_ci_timeout_seconds
// or DefaultShipCITimeoutSeconds (600) when unset. The default is applied here
// at resolution time rather than stamped onto the field, matching the
// build/exec/hook timeout resolvers.
func (c IrisToml) ResolvedShipCITimeoutSeconds() int {
	if c.ShipCITimeoutSeconds == 0 {
		return DefaultShipCITimeoutSeconds
	}
	return c.ShipCITimeoutSeconds
}

// ResolvedExecTimeoutSeconds for the exec mechanism; defaults to 30.
func (r RestartBlock) ResolvedExecTimeoutSeconds() int {
	if r.TimeoutSeconds == 0 {
		return DefaultExecTimeoutSeconds
	}
	return r.TimeoutSeconds
}
