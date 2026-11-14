package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

// runCLI executes the root command with args and returns stdout+stderr and the error.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestDogfoodHelp_ReleaseGuidance(t *testing.T) {
	for _, args := range [][]string{{"dogfood", "--help"}, {"dogfood", "claim", "--help"}, {"dogfood", "renew", "--help"}, {"set-dogfood", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, err := runCLI(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Don't hold dogfood for a user who is away.", "Don't hold dogfood while fixing a problem you found:", "get back in line with a new iris_dogfood_claim", "If the user is away and Iris asks for dogfood back or says someone is waiting, call iris_dogfood_release right away."} {
				if !strings.Contains(out, want) {
					t.Errorf("help missing %q: %s", want, out)
				}
			}
		})
	}
}

// stubClient makes newCLIClient succeed (or fail) without touching argus.
func stubClient(t *testing.T, fail bool) {
	t.Helper()
	orig := newCLIClient
	t.Cleanup(func() { newCLIClient = orig })
	newCLIClient = func(ctx context.Context) (*argus.Client, error) {
		if fail {
			return nil, context.DeadlineExceeded
		}
		return argus.New("http://127.0.0.1:1", "t"), nil
	}
}

func TestDogfoodCLI_SubcommandsRegistered(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"dogfood"})
	if err != nil || cmd.Name() != "dogfood" {
		t.Fatalf("dogfood not registered: %v", err)
	}
	got := map[string]bool{}
	for _, c := range cmd.Commands() {
		got[c.Name()] = true
	}
	for _, want := range []string{"claim", "renew", "release", "force-release", "queue-move"} {
		if !got[want] {
			t.Errorf("missing subcommand %q (have %v)", want, got)
		}
	}
}

func TestDogfoodCLI_ClaimPassesFlags(t *testing.T) {
	stubClient(t, false)
	orig := dogfoodClaimVerb
	t.Cleanup(func() { dogfoodClaimVerb = orig })
	var in verbs.DogfoodClaimInput
	dogfoodClaimVerb = func(ctx context.Context, c *argus.Client, i verbs.DogfoodClaimInput) (*verbs.DogfoodClaimResult, error) {
		in = i
		return &verbs.DogfoodClaimResult{Status: "granted", DogfoodBranch: "dev"}, nil
	}
	out, err := runCLI(t, "dogfood", "claim", "--task", "T1", "--wait", "7", "--position", "2")
	if err != nil {
		t.Fatal(err)
	}
	if in.TaskID != "T1" || in.WaitSeconds != 7 || in.Position != 2 {
		t.Fatalf("input = %+v", in)
	}
	if !strings.Contains(out, `"status": "granted"`) {
		t.Fatalf("stdout should be the JSON result, got %q", out)
	}
}

func TestDogfoodCLI_ClaimRequiresTask(t *testing.T) {
	stubClient(t, false)
	if _, err := runCLI(t, "dogfood", "claim"); err == nil || !strings.Contains(err.Error(), "task") {
		t.Fatalf("err = %v", err)
	}
}

func TestDogfoodCLI_ClaimSHA(t *testing.T) {
	stubClient(t, false)
	orig := dogfoodClaimVerb
	t.Cleanup(func() { dogfoodClaimVerb = orig })
	dogfoodClaimVerb = func(ctx context.Context, c *argus.Client, in verbs.DogfoodClaimInput) (*verbs.DogfoodClaimResult, error) {
		if in.SHA != "main" {
			t.Fatalf("SHA=%q", in.SHA)
		}
		return &verbs.DogfoodClaimResult{Status: "already_deployed", SHA: "resolved"}, nil
	}
	out, err := runCLI(t, "dogfood", "claim", "--task", "B", "--sha", "main")
	if err != nil || !strings.Contains(out, `"already_deployed"`) || !strings.Contains(out, `"sha": "resolved"`) {
		t.Fatalf("out=%s err=%v", out, err)
	}
}

func TestDogfoodCLI_RenewAndReleasePassTask(t *testing.T) {
	stubClient(t, false)
	origRenew, origRel := dogfoodRenewVerb, dogfoodReleaseVerb
	t.Cleanup(func() { dogfoodRenewVerb, dogfoodReleaseVerb = origRenew, origRel })
	var renewed, released string
	dogfoodRenewVerb = func(ctx context.Context, c *argus.Client, id string) (*verbs.DogfoodRenewResult, error) {
		renewed = id
		return &verbs.DogfoodRenewResult{Renewed: true}, nil
	}
	dogfoodReleaseVerb = func(ctx context.Context, c *argus.Client, id string) (*verbs.DogfoodReleaseResult, error) {
		released = id
		return &verbs.DogfoodReleaseResult{Released: true, Was: "holder"}, nil
	}
	if _, err := runCLI(t, "dogfood", "renew", "--task", "R1"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "dogfood", "release", "--task", "R2"); err != nil {
		t.Fatal(err)
	}
	if renewed != "R1" || released != "R2" {
		t.Fatalf("renewed=%q released=%q", renewed, released)
	}
}

func TestDogfoodCLI_ClaimNeedsArgus(t *testing.T) {
	stubClient(t, true)
	_, err := runCLI(t, "dogfood", "claim", "--task", "T1")
	if err == nil || !strings.Contains(err.Error(), "argus client required for dogfood claim") {
		t.Fatalf("err = %v", err)
	}
}

func TestDogfoodCLI_ForceReleaseWorksWithoutArgus(t *testing.T) {
	stubClient(t, true) // argus down must not block an eviction
	orig := dogfoodForceReleaseVerb
	t.Cleanup(func() { dogfoodForceReleaseVerb = orig })
	var in verbs.DogfoodForceReleaseInput
	var gotClient *argus.Client = argus.New("x", "y")
	dogfoodForceReleaseVerb = func(ctx context.Context, c *argus.Client, i verbs.DogfoodForceReleaseInput) (*verbs.DogfoodForceReleaseResult, error) {
		in, gotClient = i, c
		return &verbs.DogfoodForceReleaseResult{Released: true}, nil
	}
	if _, err := runCLI(t, "dogfood", "force-release", "--reason", "stuck", "--path", "/repo"); err != nil {
		t.Fatal(err)
	}
	if in.Reason != "stuck" || in.Path != "/repo" || gotClient != nil {
		t.Fatalf("input=%+v client=%v", in, gotClient)
	}
}

func TestDogfoodCLI_ForceReleaseValidation(t *testing.T) {
	stubClient(t, false)
	if _, err := runCLI(t, "dogfood", "force-release"); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("missing reason: %v", err)
	}
	if _, err := runCLI(t, "dogfood", "force-release", "--reason", "x", "--task", "T", "--path", "/p"); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("task+path: %v", err)
	}
}

func TestDogfoodCLI_QueueMove(t *testing.T) {
	stubClient(t, false)
	orig := dogfoodQueueMoveVerb
	t.Cleanup(func() { dogfoodQueueMoveVerb = orig })
	var in verbs.DogfoodQueueMoveInput
	dogfoodQueueMoveVerb = func(ctx context.Context, c *argus.Client, i verbs.DogfoodQueueMoveInput) (*verbs.DogfoodQueueMoveResult, error) {
		in = i
		return &verbs.DogfoodQueueMoveResult{}, nil
	}
	if _, err := runCLI(t, "dogfood", "queue-move", "Q1", "2", "--task", "T"); err != nil {
		t.Fatal(err)
	}
	if in.MoveTaskID != "Q1" || in.Position != 2 || in.TaskID != "T" {
		t.Fatalf("input = %+v", in)
	}
	for _, bad := range []string{"0", "x"} {
		if _, err := runCLI(t, "dogfood", "queue-move", "Q1", bad); err == nil || !strings.Contains(err.Error(), "positive integer") {
			t.Errorf("position %q: %v", bad, err)
		}
	}
	if _, err := runCLI(t, "dogfood", "queue-move", "Q1", "1", "--task", "T", "--path", "/p"); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("task+path: %v", err)
	}
}

func TestSetDogfoodCLI_LeaseOverrideAndForceAreIndependent(t *testing.T) {
	stubClient(t, false)
	orig := setDogfoodVerb
	t.Cleanup(func() { setDogfoodVerb = orig })
	var gotTask string
	var gotOpts verbs.SetDogfoodOpts
	setDogfoodVerb = func(ctx context.Context, c *argus.Client, task string, o verbs.SetDogfoodOpts) (*verbs.SetDogfoodResult, error) {
		gotTask, gotOpts = task, o
		return nil, nil
	}
	manifest := `{"sha":"abc"}`
	cases := []struct {
		name         string
		args         []string
		force, lease bool
	}{
		{"neither", nil, false, false},
		{"lease-override only", []string{"--lease-override"}, false, true},
		{"force only", []string{"--force"}, true, false},
		{"both", []string{"--force", "--lease-override"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"set-dogfood", "--sha", "deadbeef", "--manifest", manifest, "--task", "T9"}, tc.args...)
			if _, err := runCLI(t, args...); err != nil {
				t.Fatal(err)
			}
			if gotTask != "T9" || gotOpts.Sha != "deadbeef" || gotOpts.Force != tc.force || gotOpts.LeaseOverride != tc.lease {
				t.Fatalf("task=%q opts=%+v", gotTask, gotOpts)
			}
		})
	}
}

func TestSetDogfoodCLI_OmittedTaskIsSelfTarget(t *testing.T) {
	stubClient(t, false)
	orig := setDogfoodVerb
	t.Cleanup(func() { setDogfoodVerb = orig })
	gotTask := "unset"
	setDogfoodVerb = func(ctx context.Context, c *argus.Client, task string, o verbs.SetDogfoodOpts) (*verbs.SetDogfoodResult, error) {
		gotTask = task
		return nil, nil
	}
	if _, err := runCLI(t, "set-dogfood", "--sha", "deadbeef", "--manifest", `{}`, "--lease-override"); err != nil {
		t.Fatal(err)
	}
	if gotTask != "" {
		t.Fatalf("task = %q, want empty (self-target)", gotTask)
	}
}
