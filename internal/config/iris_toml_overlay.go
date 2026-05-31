package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

// IrisLocalTomlFilename is the conventional filename for the per-developer
// overlay declaration. It lives at the source repo root alongside
// `.iris.toml` and is expected to be gitignored. Its presence is optional;
// its absence is silent.
const IrisLocalTomlFilename = ".iris.local.toml"

// PeekLocalDogfoodBranch leniently reads ONLY dogfood_branch from the
// .iris.local.toml at repoRoot. It exists for iris:set_dogfood's bootstrap
// path: dogfood_branch is the pointer to the branch that carries .iris.toml, so
// it must be readable even when .iris.toml is ABSENT at the source root — the
// LoadOverlay path returns no doc in that case (the local file is an overlay on
// a required shared base, not a standalone config). This performs no
// validation and returns "" on any problem — missing file, malformed TOML,
// unset field.
func PeekLocalDogfoodBranch(repoRoot string) string {
	data, err := os.ReadFile(filepath.Join(repoRoot, IrisLocalTomlFilename))
	if err != nil {
		return ""
	}
	var doc IrisToml
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return ""
	}
	return doc.DogfoodBranch
}

// Source identifies which on-disk file a particular field's value came from
// in the merged result. Reported by LoadOverlay's Provenance map so that
// `iris:status` (and a human reading validate-config output) can tell a
// shared default from a developer override.
type Source string

const (
	// SourceShared marks a field whose resolved value came from `.iris.toml`
	// (the checked-in, project-wide file).
	SourceShared Source = "shared"

	// SourceLocal marks a field whose resolved value came from
	// `.iris.local.toml` (the gitignored per-developer overlay).
	SourceLocal Source = "local"
)

// Warning codes surfaced by the overlay loader. These match the spec's
// `iris-validate-config` warning identifiers so the validator can pass them
// through to clients unchanged.
const (
	// WarnLocalFieldInSharedConfig fires when a `local`-tagged field
	// (e.g. dogfood_branch) is set in `.iris.toml`. The value is still
	// honored — graceful migration — but the warning carries a hint to
	// move it into `.iris.local.toml`.
	WarnLocalFieldInSharedConfig = "local_field_in_shared_config"

	// WarnSharedFieldInLocalConfig fires when a `shared`-tagged field
	// (e.g. default_branch, build) is set in `.iris.local.toml`. The
	// local value is IGNORED — the shared file's value wins — and the
	// warning surfaces the misplacement.
	WarnSharedFieldInLocalConfig = "shared_field_in_local_config"
)

// OverlayWarning is a structured warning surfaced by the overlay loader.
//
// Warnings are non-fatal: parsing succeeded, the merged doc is usable, and
// the validator may choose to display them alongside `valid: true`. They
// describe taxonomy violations (a field appearing in the wrong file), not
// schema errors.
type OverlayWarning struct {
	// Code identifies the warning class (WarnLocalFieldInSharedConfig or
	// WarnSharedFieldInLocalConfig).
	Code string `json:"code"`

	// Field is the top-level TOML field name (e.g. "dogfood_branch",
	// "default_branch", "build") that triggered the warning. Matches the
	// `toml:"..."` tag on the IrisToml struct, not the Go field name.
	Field string `json:"field"`

	// File is the on-disk filename where the misplaced field was found
	// (one of IrisTomlFilename, IrisLocalTomlFilename).
	File string `json:"file"`

	// Message is a human-readable description of the misplacement.
	Message string `json:"message"`

	// Hint, when set, carries an actionable remediation step
	// (e.g. "move dogfood_branch to .iris.local.toml"). Always populated
	// for the migration warning.
	Hint string `json:"hint,omitempty"`
}

// OverlayResult bundles the outputs of a single overlay-load call. Its
// shape is deliberately verbose so future callers (validate_config, status)
// can take only the slice they care about without re-running the loader.
type OverlayResult struct {
	// Doc is the merged IrisToml after applying the local overlay onto
	// the shared base. Nil when `.iris.toml` is missing or when the
	// shared file failed to parse (in which case ValidationErrors
	// describes the failure).
	Doc *IrisToml

	// Provenance maps each top-level TOML field name that was SET in at
	// least one file to the source it ended up coming from. Fields unset
	// in both files are omitted (per design.md: "A field that's unset in
	// both files should probably be omitted from `config_sources`").
	// Always non-nil — an empty map when nothing was set.
	Provenance map[string]Source

	// Warnings carries taxonomy-violation warnings: a local-tagged field
	// in the shared file or a shared-tagged field in the local file.
	// May be nil/empty when both files respect the taxonomy.
	Warnings []OverlayWarning

	// ValidationErrors aggregates schema/parse errors from both files
	// AND cross-validation errors from the merged Doc. Stage 3 will
	// surface these through validate_config. Order: shared-file errors
	// first, then local-file errors, then merged-doc cross-validation.
	ValidationErrors []ValidationError
}

// LoadOverlay reads `.iris.toml` and an optional `.iris.local.toml` from
// repoRoot, merges them, tracks per-field provenance, and reports any
// taxonomy violations as warnings.
//
// Merge semantics, driven by the `kind` struct tags on IrisToml:
//   - `local`-tagged fields: the local file wins. If the shared file ALSO
//     sets the field, a WarnLocalFieldInSharedConfig warning fires (the
//     field belongs in the local file). Either way the value is honored.
//   - `shared`-tagged fields: the shared file wins. If the local file
//     ALSO sets the field, a WarnSharedFieldInLocalConfig warning fires
//     and the local value is IGNORED.
//
// isSelf is forwarded to the underlying Validate call (controls whether
// `exit_code` is a legal restart mechanism — see iris_toml.go).
//
// Returns a populated OverlayResult and a nil error on success. Returns a
// non-nil error only for I/O failures unrelated to file absence; parse
// failures land in ValidationErrors instead so the caller can keep
// rendering the partially-loaded config.
func LoadOverlay(repoRoot string, isSelf bool) (*OverlayResult, error) {
	out := &OverlayResult{Provenance: map[string]Source{}}

	sharedPath := filepath.Join(repoRoot, IrisTomlFilename)
	localPath := filepath.Join(repoRoot, IrisLocalTomlFilename)

	// 1) Load the shared file. Missing is silent; parse failures land in
	//    ValidationErrors and short-circuit the merge (no point overlaying
	//    onto a doc we couldn't parse).
	sharedDoc, sharedSetFields, sharedErrs, ioErr := readAndDecode(sharedPath)
	if ioErr != nil {
		return out, ioErr
	}
	out.ValidationErrors = append(out.ValidationErrors, sharedErrs...)
	if sharedDoc == nil {
		// Shared file missing or unparseable. We do NOT attempt to load
		// the local file as a standalone config: the local file is an
		// OVERLAY, not a fallback, and IrisToml requires fields the local
		// file is not allowed to set. Returning early matches
		// LoadIrisToml's missing-file contract (nil doc, no error).
		return out, nil
	}

	// 2) Walk the shared doc's fields. Every set field gets a "shared"
	//    provenance entry. Local-tagged fields set in the shared file
	//    also trigger the migration warning.
	for _, name := range sharedSetFields {
		kind, known := FieldKind(name)
		if !known {
			continue
		}
		out.Provenance[name] = SourceShared
		if kind == FieldKindLocal {
			out.Warnings = append(out.Warnings, OverlayWarning{
				Code:    WarnLocalFieldInSharedConfig,
				Field:   name,
				File:    IrisTomlFilename,
				Message: fmt.Sprintf("local-tagged field %q is set in %s", name, IrisTomlFilename),
				Hint:    fmt.Sprintf("move %s to %s", name, IrisLocalTomlFilename),
			})
		}
	}

	// 3) Load the local file. Missing is silent; parse failures land in
	//    ValidationErrors but the shared doc is preserved (spec scenario
	//    "Malformed local file surfaces a structured error: .iris.toml's
	//    fields are NOT silently lost").
	localDoc, localSetFields, localErrs, ioErr := readAndDecode(localPath)
	if ioErr != nil {
		// I/O errors other than ENOENT bubble up. Caller still gets the
		// shared doc and any warnings we accumulated.
		out.Doc = sharedDoc
		return out, ioErr
	}
	out.ValidationErrors = append(out.ValidationErrors, localErrs...)

	// 4) Apply the overlay if the local file parsed (or was absent).
	merged := *sharedDoc
	if localDoc != nil {
		for _, name := range localSetFields {
			kind, known := FieldKind(name)
			if !known {
				// Unknown TOML field in the local file. The decoder
				// already emitted an "unknown field" ValidationError;
				// nothing to do for the merge.
				continue
			}
			switch kind {
			case FieldKindLocal:
				// Local wins. Copy the field from localDoc onto merged.
				copyFieldByTOMLName(&merged, localDoc, name)
				out.Provenance[name] = SourceLocal
			case FieldKindShared:
				// Shared wins; local value is ignored. Warn.
				out.Warnings = append(out.Warnings, OverlayWarning{
					Code:    WarnSharedFieldInLocalConfig,
					Field:   name,
					File:    IrisLocalTomlFilename,
					Message: fmt.Sprintf("shared-tagged field %q is set in %s and will be ignored", name, IrisLocalTomlFilename),
					Hint:    fmt.Sprintf("move %s to %s, or remove it from %s", name, IrisTomlFilename, IrisLocalTomlFilename),
				})
				// Provenance remains "shared" only if the shared file set
				// the field; otherwise the field is effectively unset in
				// the merged doc (we did not copy the local value).
			}
		}
	}
	out.Doc = &merged

	return out, nil
}

// readAndDecode reads the TOML file at path and returns the parsed doc, the
// list of TOML field names actually present in the file (top-level only),
// any ValidationErrors produced by parsing, and an I/O error for cases
// other than file-absent.
//
// On ENOENT returns (nil, nil, nil, nil) — the caller decides whether
// absence is OK.
//
// On parse error returns (nil, nil, []ValidationError{...}, nil) — the
// caller can render the parse error and continue without a doc.
//
// On unknown-field error returns (doc, setFields, []ValidationError{...},
// nil) — the doc is usable, the errors describe which fields were ignored.
func readAndDecode(path string) (*IrisToml, []string, []ValidationError, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, fmt.Errorf("read %s: %w", path, err)
	}

	var doc IrisToml
	meta, decodeErr := toml.Decode(string(data), &doc)
	if decodeErr != nil {
		ve := ValidationError{
			Field:   filepath.Base(path),
			Message: fmt.Sprintf("TOML parse error: %v", decodeErr),
			Hint:    "fix the TOML syntax",
		}
		if line := tomlErrorLine(decodeErr); line > 0 {
			ve.Line = line
		}
		return nil, nil, []ValidationError{ve}, nil
	}

	var errs []ValidationError
	for _, key := range meta.Undecoded() {
		errs = append(errs, ValidationError{
			Field:   key.String(),
			Message: "unknown field",
			Hint:    "remove the field or check for a typo against the .iris.toml schema",
		})
	}

	// Collect the set of top-level TOML field names actually decoded.
	// meta.Keys() includes every key TOML saw — including nested ones —
	// so we filter to top-level (single-segment) keys and intersect with
	// the IrisToml schema to avoid surprises from unknown top-levels.
	setFields := topLevelKeysFromMeta(meta)

	return &doc, setFields, errs, nil
}

// topLevelKeysFromMeta returns the set of top-level TOML field names that
// were present in the decoded file. Only keys that map to a recognised
// top-level field on IrisToml are returned (unknown fields are filtered out
// — the caller has already turned them into ValidationErrors).
//
// We use meta.Keys() (rather than reflecting on the decoded struct) so we
// can distinguish "field was set in TOML" from "field is at the Go zero
// value because it was omitted". This matters: `default_branch = ""` set
// explicitly in TOML and `default_branch` omitted both produce the empty
// string on the struct, but only the former should be tracked as set.
func topLevelKeysFromMeta(meta toml.MetaData) []string {
	known := map[string]struct{}{}
	for _, name := range append(SharedFields(), LocalFields()...) {
		known[name] = struct{}{}
	}
	seen := map[string]bool{}
	var out []string
	for _, k := range meta.Keys() {
		segs := []string(k)
		if len(segs) == 0 {
			continue
		}
		top := segs[0]
		if seen[top] {
			continue
		}
		if _, ok := known[top]; !ok {
			continue
		}
		seen[top] = true
		out = append(out, top)
	}
	return out
}

// copyFieldByTOMLName copies a single top-level field from src onto dst,
// addressed by the `toml:"..."` tag name (not the Go field name).
//
// The taxonomy IS the source of truth: callers determine WHICH fields to
// copy via FieldKind. This helper only handles the mechanical move.
//
// Both dst and src must be *IrisToml. The src field is copied by value
// (the IrisToml struct contains no pointers that require deep-copy at the
// field granularity used here — pointer fields like PreFlight are blocks
// and get whole-pointer copy semantics, which is exactly what we want for
// the table-level overlay).
func copyFieldByTOMLName(dst, src *IrisToml, tomlName string) {
	dstV := reflect.ValueOf(dst).Elem()
	srcV := reflect.ValueOf(src).Elem()
	typ := dstV.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("toml")
		if tag == "" || tag == "-" {
			continue
		}
		if strings.SplitN(tag, ",", 2)[0] != tomlName {
			continue
		}
		if dstV.Field(i).CanSet() {
			dstV.Field(i).Set(srcV.Field(i))
		}
		return
	}
}
