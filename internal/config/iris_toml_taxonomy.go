package config

import (
	"reflect"
	"sort"
	"strings"
)

// FieldKindValue identifies whether a top-level `.iris.toml` field is meant
// to live in the project-wide config (shared) or in each developer's
// gitignored overlay (local).
//
// The taxonomy is declared directly on the IrisToml struct via the `kind`
// struct tag. The functions in this file expose that classification to
// other packages and guarantee — via the companion test — that every
// top-level field on IrisToml is classified exactly once.
//
// Stage 2 (loader overlay) consumes FieldKind to decide whether a value
// from .iris.local.toml may override the .iris.toml value, and to emit
// `local_field_in_shared_config` / `shared_field_in_local_config`
// warnings during validation.
type FieldKindValue string

const (
	// FieldKindShared marks a field as project-wide. It belongs in
	// `.iris.toml`, is checked in, and is identical for every developer.
	FieldKindShared FieldKindValue = "shared"

	// FieldKindLocal marks a field as per-developer. It belongs in
	// `.iris.local.toml`, is gitignored, and may differ from one
	// developer to the next.
	FieldKindLocal FieldKindValue = "local"
)

// fieldKindStructTag is the name of the struct tag the IrisToml schema
// uses to declare each field's taxonomy kind. Centralised so the helper
// and its tests share a single spelling.
const fieldKindStructTag = "kind"

// FieldKind reports the taxonomy kind of the given top-level TOML field
// name (as it would appear in `.iris.toml` / `.iris.local.toml`).
//
// Returns (kind, true) for any field declared on the IrisToml struct.
// Returns ("", false) for any other name. Callers MUST treat the
// unrecognised case as a typo or schema mismatch, not as "neither shared
// nor local".
//
// The classification source of truth is the `kind` struct tag on each
// IrisToml field. The companion test asserts every exported field of
// IrisToml carries a valid kind, so adding a new field to the schema
// without classifying it fails CI.
func FieldKind(tomlFieldName string) (FieldKindValue, bool) {
	kind, ok := fieldKinds()[tomlFieldName]
	return kind, ok
}

// SharedFields returns the TOML names of every field tagged
// FieldKindShared, in sorted order. The slice is freshly allocated on
// every call, so callers may mutate it without affecting the package's
// internal state.
func SharedFields() []string {
	return fieldsByKind(FieldKindShared)
}

// LocalFields returns the TOML names of every field tagged
// FieldKindLocal, in sorted order. The slice is freshly allocated on
// every call.
func LocalFields() []string {
	return fieldsByKind(FieldKindLocal)
}

// fieldKinds builds the taxonomy map by reflecting on IrisToml.
//
// We compute on every call rather than memoizing so the test harness can
// reset state cleanly and so a future code-generator / linter doesn't
// have to worry about init ordering. The map has fewer than a dozen
// entries; cost is irrelevant next to the safety of a stateless helper.
func fieldKinds() map[string]FieldKindValue {
	out := map[string]FieldKindValue{}
	typ := reflect.TypeOf(IrisToml{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		tomlTag := f.Tag.Get("toml")
		if tomlTag == "" || tomlTag == "-" {
			continue
		}
		name := strings.SplitN(tomlTag, ",", 2)[0]

		kindTag := f.Tag.Get(fieldKindStructTag)
		switch FieldKindValue(kindTag) {
		case FieldKindShared, FieldKindLocal:
			out[name] = FieldKindValue(kindTag)
		default:
			// Unclassified or invalid kind — deliberately omitted so the
			// exhaustiveness test fails loudly with a clear message
			// rather than silently treating the field as one or the
			// other.
		}
	}
	return out
}

// fieldsByKind returns the sorted TOML names of every field whose
// taxonomy kind equals want.
func fieldsByKind(want FieldKindValue) []string {
	kinds := fieldKinds()
	out := make([]string, 0, len(kinds))
	for name, k := range kinds {
		if k == want {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
