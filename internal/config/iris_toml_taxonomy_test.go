package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestFieldKind_ClassifiesEveryTopLevelTOMLField asserts the helper returns a
// concrete `shared` or `local` kind for every TOML field name that the
// IrisToml struct exposes. New fields added to the struct without a
// classification will fail this test.
//
// This is the exhaustiveness guarantee promised by Stage 1: the taxonomy
// must cover every top-level field; no `unknown` is allowed.
func TestFieldKind_ClassifiesEveryTopLevelTOMLField(t *testing.T) {
	want := topLevelTOMLFieldNames(t)

	for _, name := range want {
		kind, ok := FieldKind(name)
		if !ok {
			t.Errorf("FieldKind(%q) returned ok=false; every field of IrisToml must be classified", name)
			continue
		}
		switch kind {
		case FieldKindShared, FieldKindLocal:
			// fine
		default:
			t.Errorf("FieldKind(%q) returned kind=%q; want %q or %q", name, kind, FieldKindShared, FieldKindLocal)
		}
	}
}

// TestFieldKind_ExpectedShared pins the initial taxonomy: these fields must
// be classified as shared (project-wide).
func TestFieldKind_ExpectedShared(t *testing.T) {
	shared := []string{
		"schema_version",
		"default_branch",
		"build",
		"restart",
		"pre_flight",
		"verify",
		"post_merge",
		"git_transfer_timeout_seconds",
	}
	for _, name := range shared {
		kind, ok := FieldKind(name)
		if !ok {
			t.Errorf("FieldKind(%q): ok=false; want ok=true,kind=%q", name, FieldKindShared)
			continue
		}
		if kind != FieldKindShared {
			t.Errorf("FieldKind(%q) = %q; want %q", name, kind, FieldKindShared)
		}
	}
}

// TestFieldKind_ExpectedLocal pins the initial taxonomy: these fields must
// be classified as local (per-developer).
func TestFieldKind_ExpectedLocal(t *testing.T) {
	local := []string{
		"dogfood_branch",
		"ship_ci_timeout_seconds",
	}
	for _, name := range local {
		kind, ok := FieldKind(name)
		if !ok {
			t.Errorf("FieldKind(%q): ok=false; want ok=true,kind=%q", name, FieldKindLocal)
			continue
		}
		if kind != FieldKindLocal {
			t.Errorf("FieldKind(%q) = %q; want %q", name, kind, FieldKindLocal)
		}
	}
}

// TestFieldKind_NoFieldInBothLists guards the simple rule "a field is
// either shared or local, never both". Implementation bugs in a future
// switch/map could classify the same field twice; this test catches that.
func TestFieldKind_NoFieldInBothLists(t *testing.T) {
	for _, name := range topLevelTOMLFieldNames(t) {
		shared := FieldKindOfMust(t, name) == FieldKindShared
		local := FieldKindOfMust(t, name) == FieldKindLocal
		if shared == local {
			// either both true (impossible since FieldKind returns one value)
			// or both false (FieldKind returned a third value)
			t.Errorf("FieldKind(%q) is neither cleanly shared nor cleanly local", name)
		}
	}
}

// TestFieldKind_UnknownFieldReturnsNotOK verifies the helper is closed: an
// unrelated field name returns ok=false (so callers can detect typos /
// unknown fields rather than silently treating them as shared or local).
func TestFieldKind_UnknownFieldReturnsNotOK(t *testing.T) {
	if kind, ok := FieldKind("not_a_real_field"); ok {
		t.Errorf(`FieldKind("not_a_real_field") = (%q, true); want (_, false)`, kind)
	}
}

// TestSharedFields_MatchesExpectedSet documents the shared classification
// explicitly so a reviewer can read the test and see the full list.
func TestSharedFields_MatchesExpectedSet(t *testing.T) {
	got := append([]string(nil), SharedFields()...)
	want := []string{
		"build",
		"default_branch",
		"git_transfer_timeout_seconds",
		"post_merge",
		"pre_flight",
		"restart",
		"schema_version",
		"verify",
	}
	sortStrings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SharedFields() = %v\nwant: %v", got, want)
	}
}

// TestLocalFields_MatchesExpectedSet documents the local classification
// explicitly.
func TestLocalFields_MatchesExpectedSet(t *testing.T) {
	got := append([]string(nil), LocalFields()...)
	want := []string{
		"dogfood_branch",
		"ship_ci_timeout_seconds",
	}
	sortStrings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LocalFields() = %v\nwant: %v", got, want)
	}
}

// TestSharedAndLocalFields_Disjoint asserts the two lists do not overlap.
// Cross-checked against the IrisToml field set so a future field that's
// accidentally listed in both, or in neither, fails loudly.
func TestSharedAndLocalFields_Disjoint(t *testing.T) {
	sharedSet := map[string]struct{}{}
	for _, n := range SharedFields() {
		sharedSet[n] = struct{}{}
	}
	for _, n := range LocalFields() {
		if _, dup := sharedSet[n]; dup {
			t.Errorf("field %q appears in both SharedFields() and LocalFields()", n)
		}
	}

	// Union must equal the full top-level TOML field set (exhaustiveness
	// expressed via the two list accessors).
	union := map[string]struct{}{}
	for _, n := range SharedFields() {
		union[n] = struct{}{}
	}
	for _, n := range LocalFields() {
		union[n] = struct{}{}
	}
	for _, name := range topLevelTOMLFieldNames(t) {
		if _, ok := union[name]; !ok {
			t.Errorf("field %q is in IrisToml but missing from SharedFields() + LocalFields()", name)
		}
	}
	if len(union) != len(topLevelTOMLFieldNames(t)) {
		t.Errorf("SharedFields()+LocalFields() count=%d; IrisToml top-level field count=%d",
			len(union), len(topLevelTOMLFieldNames(t)))
	}
}

// --- helpers ---------------------------------------------------------------

// topLevelTOMLFieldNames returns the TOML tag names of every exported field
// on the IrisToml struct, via reflection. This is the authoritative
// definition of "every field in the schema" used by the exhaustiveness
// tests above.
func topLevelTOMLFieldNames(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(IrisToml{})
	names := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("toml")
		if tag == "" || tag == "-" {
			t.Fatalf("IrisToml field %q has no toml tag; taxonomy cannot classify it", f.Name)
		}
		// TOML tags in this codebase are single names, but trim defensively.
		name := strings.SplitN(tag, ",", 2)[0]
		names = append(names, name)
	}
	return names
}

// FieldKindOfMust is a test helper that fails the test if FieldKind returns
// ok=false. It exists so the "no field in both lists" assertion reads
// cleanly.
func FieldKindOfMust(t *testing.T, name string) FieldKindValue {
	t.Helper()
	k, ok := FieldKind(name)
	if !ok {
		t.Fatalf("FieldKind(%q): ok=false; field is not classified", name)
	}
	return k
}

// sortStrings is a tiny in-place sort to keep the table-driven tests above
// stable without pulling in sort.Strings at the top.
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j-1] > ss[j]; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}
