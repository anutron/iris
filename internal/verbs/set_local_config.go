// Package verbs: set_local_config implements iris:set_local_config — write
// (or merge into) the per-developer overlay `.iris.local.toml` at the source
// repo root with worker-supplied local-tagged fields.
//
// The verb is the symmetric write counterpart to iris:status's read surface.
// Sandboxed argus workers can't reach outside their worktree to edit
// `.iris.local.toml` themselves, so this verb is the only sanctioned way for
// a worker to set its own dogfood_branch / ship_ci_timeout_seconds.
//
// Refuse-then-write discipline: every input field is validated for taxonomy
// (must be local-tagged), schema (must be a known field), and value (must
// pass the same per-field validators the loader uses) BEFORE the file is
// touched. A single bad field aborts the entire call with no writes.
//
// See openspec/changes/add-iris-local-toml-overlay/design.md and
// specs/iris-set-local-config/spec.md.

package verbs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// SetLocalConfigOpts is the typed input for SetLocalConfig.
type SetLocalConfigOpts struct {
	// Fields is the map of local-tagged field names to values to set in
	// `.iris.local.toml`. Empty or nil means "no sets".
	Fields map[string]any
	// Delete is the list of local-tagged field names to remove from
	// `.iris.local.toml`. Names not currently present in the file are
	// silently ignored. Empty or nil means "no deletes".
	Delete []string
}

// SetLocalConfigResult is the structured success payload, mirrored to MCP
// and CLI as pretty-printed JSON.
type SetLocalConfigResult struct {
	// Written is true when the verb wrote (or re-wrote) the file. The
	// idempotent re-set scenario still reports Written=true because the
	// rename happened.
	Written bool `json:"written"`
	// Path is the absolute path to `.iris.local.toml` at the source repo
	// root (canonicalized).
	Path string `json:"path"`
	// Resolved is the contents of the file AFTER the write: just the
	// local toml's top-level fields, NOT a merge against `.iris.toml`.
	Resolved map[string]any `json:"resolved"`
	// Warnings is the list of non-fatal warnings (currently unused; the
	// per-field validators refuse rather than warn).
	Warnings []string `json:"warnings"`
}

// SetLocalConfigError is the structured error returned when input validation
// refuses a write. Callers wanting the structured shape can type-assert via
// errors.As; the MCP handler surfaces Code/Field/Hint to clients.
type SetLocalConfigError struct {
	// Code identifies the refusal class:
	//   - "field_not_local"  – name was known but not local-tagged
	//   - "unknown_field"    – name was not a known IrisToml field
	//   - "invalid_value"    – value failed per-field validation
	Code string
	// Field is the offending input field name.
	Field string
	// Message is a short human description (populated for invalid_value).
	Message string
	// Hint is an actionable remediation step.
	Hint string
}

// Error implements the error interface for SetLocalConfigError.
func (e *SetLocalConfigError) Error() string {
	parts := []string{e.Code}
	if e.Field != "" {
		parts = append(parts, "field="+e.Field)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Hint != "" {
		parts = append(parts, "hint: "+e.Hint)
	}
	return strings.Join(parts, ": ")
}

// SetLocalConfig writes (or merges into) `.iris.local.toml` at the resolved
// source repo's root with the worker-supplied local-tagged fields.
//
// Sequence:
//  1. Resolve the target source repo.
//  2. Validate every input field name is `local`-tagged. A shared-tagged
//     field (or one in delete) refuses with field_not_local.
//  3. Validate every input field is a known IrisToml field. Unknown names
//     refuse with unknown_field.
//  4. Validate each value against the loader's per-field rules. Refuse with
//     invalid_value on any failure (including dogfood_branch == default_branch).
//  5. Acquire the source-repo lock for the read-modify-write.
//  6. Read the existing `.iris.local.toml` (treat ENOENT as empty doc),
//     apply deletes, apply sets.
//  7. Marshal back to TOML and atomically rename (tmp + rename).
//  8. Return the contents of the file after the write.
//
// Per the spec, validation is purely per-field — set_local_config does NOT
// cross-validate against `.iris.toml`. The local file may exist standalone.
func SetLocalConfig(ctx context.Context, client *argus.Client, taskID string, opts SetLocalConfigOpts) (*SetLocalConfigResult, error) {
	// 1. Resolve target. No allowlist bypass: ResolveTarget enforces it
	// for task_id / path inputs, and the self path (taskID == "" and no
	// path) is implicit-iris-on-iris.
	target, err := ResolveTarget(ctx, client, taskID, "")
	if err != nil {
		return nil, err
	}

	// 2 + 3. Taxonomy + known-field validation for the union of Fields
	// and Delete names. We do this BEFORE touching the lock or filesystem
	// so refusals are fully side-effect-free.
	if err := validateLocalConfigNames(opts); err != nil {
		return nil, err
	}

	// 4. Per-field value validation. dogfood_branch's "must not equal
	// default_branch" check requires resolving origin/HEAD, which is a
	// pure read (no lock).
	if err := validateLocalConfigValues(ctx, target.SourceRepo, opts.Fields); err != nil {
		return nil, err
	}

	// 5. Acquire the source-repo lock for the read-modify-write. Same
	// mutex set_dogfood / publish / ship_feature use, so concurrent
	// callers serialize.
	mu := lockSourceRepo(target.SourceRepo)
	defer mu.Unlock()

	dst := filepath.Join(target.SourceRepo, config.IrisLocalTomlFilename)

	// 6. Read existing file (treat absent as empty). We read into a
	// map[string]any so unknown fields and TOML comments aren't
	// stripped on a subsequent write — but since the per-field name
	// validator already rejects unknown names from agent input, any
	// pre-existing unknown fields in the file are preserved as-is.
	existing := map[string]any{}
	data, err := os.ReadFile(dst)
	switch {
	case err == nil:
		if _, derr := toml.Decode(string(data), &existing); derr != nil {
			return nil, fmt.Errorf("read %s: TOML parse error: %w", dst, derr)
		}
	case errors.Is(err, fs.ErrNotExist):
		// Empty doc — leave existing as {}.
	default:
		return nil, fmt.Errorf("read %s: %w", dst, err)
	}

	// Apply deletes first (per the design: removals before sets so a
	// caller can simultaneously delete and re-set without ambiguity).
	for _, name := range opts.Delete {
		delete(existing, name)
	}
	for name, val := range opts.Fields {
		existing[name] = val
	}

	// 7. Marshal back to TOML and atomically rename. We pre-create the
	// tmp file in the same directory so the rename is on the same fs.
	out, err := marshalLocalToml(existing)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", dst, err)
	}
	if err := atomicWriteFile(dst, out, 0o644); err != nil {
		return nil, err
	}

	return &SetLocalConfigResult{
		Written:  true,
		Path:     dst,
		Resolved: existing,
		Warnings: []string{},
	}, nil
}

// validateLocalConfigNames refuses any name in opts.Fields or opts.Delete
// that isn't a known IrisToml field or isn't local-tagged. Order: every
// known name is checked for the taxonomy violation FIRST, so an unknown
// name (which can't have a kind) is reported with unknown_field rather
// than field_not_local.
//
// Names are iterated in deterministic order so concurrent callers can
// reproduce error messages bit-for-bit on retries.
func validateLocalConfigNames(opts SetLocalConfigOpts) error {
	// Collect the set of names to check, deduplicating across Fields and
	// Delete. Sorting gives deterministic refusal order.
	names := map[string]struct{}{}
	for name := range opts.Fields {
		names[name] = struct{}{}
	}
	for _, name := range opts.Delete {
		names[name] = struct{}{}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	for _, name := range sorted {
		kind, known := config.FieldKind(name)
		if !known {
			return &SetLocalConfigError{
				Code:  "unknown_field",
				Field: name,
				Hint:  "valid local fields: " + strings.Join(config.LocalFields(), ", "),
			}
		}
		if kind != config.FieldKindLocal {
			return &SetLocalConfigError{
				Code:  "field_not_local",
				Field: name,
				Hint:  fmt.Sprintf("%s is a shared field; edit %s directly", name, config.IrisTomlFilename),
			}
		}
	}
	return nil
}

// validateLocalConfigValues runs the per-field validators on each value in
// fields. dogfood_branch additionally checks that the value does not equal
// the source repo's default branch (resolved via origin/HEAD).
func validateLocalConfigValues(ctx context.Context, sourceRepo string, fields map[string]any) error {
	// Iterate in deterministic order so the first refusal is stable.
	names := make([]string, 0, len(fields))
	for n := range fields {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		val := fields[name]
		switch name {
		case "dogfood_branch":
			s, ok := val.(string)
			if !ok {
				return &SetLocalConfigError{
					Code:    "invalid_value",
					Field:   name,
					Message: fmt.Sprintf("expected string, got %T", val),
					Hint:    "use a single ref name like \"dev\"",
				}
			}
			if !config.ValidGitBranchName(s) {
				return &SetLocalConfigError{
					Code:    "invalid_value",
					Field:   name,
					Message: "invalid git branch name",
					Hint:    "use a single ref name without spaces or invalid characters",
				}
			}
			// Cross-check against default_branch. We use the same helper
			// validate_config / set_dogfood use; a missing origin/HEAD
			// surfaces as an error to the caller (set_local_config can't
			// guess any more than the rest of iris).
			defaultBranch, err := DefaultBranch(ctx, sourceRepo)
			if err != nil {
				return err
			}
			if s == defaultBranch {
				return &SetLocalConfigError{
					Code:    "invalid_value",
					Field:   name,
					Message: "must not equal default_branch",
					Hint:    "choose a distinct branch name like \"dev\"; the origin-first model keeps the default branch read-only",
				}
			}
		case "ship_ci_timeout_seconds":
			n, ok := coerceInt64(val)
			if !ok {
				return &SetLocalConfigError{
					Code:    "invalid_value",
					Field:   name,
					Message: fmt.Sprintf("expected integer, got %T", val),
					Hint:    "use a non-negative number of seconds",
				}
			}
			if n < 0 {
				return &SetLocalConfigError{
					Code:    "invalid_value",
					Field:   name,
					Message: "must be non-negative",
					Hint:    "use a non-negative number of seconds, or omit the field to use the default of 600",
				}
			}
		default:
			// Future local fields will land here. Fail closed so a new
			// local-tagged field added to IrisToml without a value
			// validator can't smuggle anything through.
			return &SetLocalConfigError{
				Code:    "invalid_value",
				Field:   name,
				Message: "no value validator registered",
				Hint:    "iris does not yet know how to validate this field; update set_local_config",
			}
		}
	}
	return nil
}

// coerceInt64 accepts the integer-ish shapes JSON / TOML decode into Go any:
// int, int64, float64 (whole values). Anything else returns ok=false.
func coerceInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case int32:
		return int64(x), true
	case float64:
		if x == float64(int64(x)) {
			return int64(x), true
		}
		return 0, false
	}
	return 0, false
}

// marshalLocalToml renders a map[string]any to TOML bytes. The map is the
// in-memory representation of `.iris.local.toml`; we marshal it via
// BurntSushi/toml's encoder which produces deterministic, key-sorted output
// for flat scalar maps.
//
// Empty maps marshal to an empty byte slice rather than a stray newline.
func marshalLocalToml(doc map[string]any) ([]byte, error) {
	if len(doc) == 0 {
		return []byte{}, nil
	}
	var buf strings.Builder
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// atomicWriteFile writes data to a sibling .tmp file, fsyncs it, and renames
// it over dst. On any failure the tmp file is best-effort removed so the
// next run doesn't see a partial leftover.
func atomicWriteFile(dst string, data []byte, mode os.FileMode) error {
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("open tmp %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsync tmp %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, dst, err)
	}
	return nil
}
