package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunChecks_ArgsArePassedAsSeparateArgv(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-args-argv")
	// One line per argv entry, so spaces and metacharacters stay visible.
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"),
		"#!/usr/bin/env bash\necho \"argc=$#\"\nfor a in \"$@\"; do echo \"[$a]\"; done\n")
	client := stubArgus(t, src, wt)

	args := []string{"spec/models/user_spec.rb", "spec/a b.rb", "spec/x.rb; touch pwned", "$(id)", "*.rb"}
	result, err := RunChecks(context.Background(), client, "task-args", RunChecksOptions{Check: "test", Args: args})
	if err != nil {
		t.Fatalf("run checks: %v", err)
	}
	for _, want := range []string{"argc=6", "[test]", "[spec/models/user_spec.rb]", "[spec/a b.rb]", "[spec/x.rb; touch pwned]", "[$(id)]", "[*.rb]"} {
		if !strings.Contains(result.Output, want) {
			t.Errorf("output missing %q:\n%s", want, result.Output)
		}
	}
	if _, statErr := os.Stat(filepath.Join(wt, "pwned")); statErr == nil {
		t.Fatal("an arg was interpreted by a shell")
	}
	wantCmd := "script/iris-check test spec/models/user_spec.rb spec/a b.rb spec/x.rb; touch pwned $(id) *.rb"
	if result.Command != wantCmd {
		t.Errorf("command = %q, want %q", result.Command, wantCmd)
	}
}

func TestRunChecks_OmittedArgsBehaveAsBefore(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-args-none")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"), "#!/usr/bin/env bash\necho \"argc=$#\"\n")
	client := stubArgus(t, src, wt)

	for _, args := range [][]string{nil, {}} {
		result, err := RunChecks(context.Background(), client, "task-noargs", RunChecksOptions{Check: "lint", Args: args})
		if err != nil {
			t.Fatalf("run checks: %v", err)
		}
		if !strings.Contains(result.Output, "argc=1") || result.Command != "script/iris-check lint" {
			t.Fatalf("unexpected result for args=%#v: %+v", args, result)
		}
	}
}

func TestValidateCheckArgs(t *testing.T) {
	t.Parallel()
	maxLen := strings.Repeat("a", MaxCheckArgBytes)
	atLimit := make([]string, MaxCheckArgs)
	for i := range atLimit {
		atLimit[i] = maxLen
	}

	cases := []struct {
		name    string
		args    []string
		wantErr string // substring; empty means valid
	}{
		{"nil", nil, ""},
		{"empty list", []string{}, ""},
		{"plain paths", []string{"spec/a_spec.rb", "./-dash.rb"}, ""},
		{"dash inside is fine", []string{"spec/a-b_spec.rb"}, ""},
		{"leading dash", []string{"--require", "evil.rb"}, "may not start with `-`"},
		{"single dash", []string{"-"}, "may not start with `-`"},
		{"NUL byte", []string{"spec/a\x00b.rb"}, "NUL"},
		{"empty arg", []string{"spec/a_spec.rb", ""}, "may not be empty"},
		{"too many", make([]string, MaxCheckArgs+1), "at most 64"},
		{"arg too long", []string{maxLen + "a"}, "at most 512 bytes"},
		{"exactly at both limits", atLimit, ""},
	}
	for _, tc := range cases {
		if tc.name == "too many" {
			for i := range tc.args {
				tc.args[i] = "a"
			}
		}
		err := ValidateCheckArgs(tc.args)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s: error = %v, want substring %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestValidateCheckArgs_NamesOffendingArg(t *testing.T) {
	t.Parallel()
	err := ValidateCheckArgs([]string{"ok.rb", "--require"})
	if err == nil || !strings.Contains(err.Error(), "--require") {
		t.Fatalf("error should name the offending arg, got %v", err)
	}
}

// Invalid args must be rejected before anything runs.
func TestRunChecks_InvalidArgsRunNothing(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-args-bad")
	ran := filepath.Join(t.TempDir(), "ran")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"), "#!/usr/bin/env bash\ntouch '"+ran+"'\n")
	client := stubArgus(t, src, wt)

	result, err := RunChecks(context.Background(), client, "task-badargs", RunChecksOptions{Check: "test", Args: []string{"--require", "evil.rb"}})
	if err == nil || result != nil {
		t.Fatalf("expected an error and no result, got %+v, %v", result, err)
	}
	if _, statErr := os.Stat(ran); statErr == nil {
		t.Fatal("script ran despite invalid args")
	}
}
