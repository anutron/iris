package main

import (
	"context"
	"strings"
	"testing"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

func TestDogfoodDeployCLI(t *testing.T) {
	stubClient(t, false)
	original := dogfoodDeployVerb
	t.Cleanup(func() { dogfoodDeployVerb = original })
	var got verbs.DogfoodDeployOpts
	dogfoodDeployVerb = func(ctx context.Context, client *argus.Client, task string, in verbs.DogfoodDeployOpts) (*verbs.SetDogfoodResult, error) {
		if task != "T" {
			t.Fatalf("task=%q", task)
		}
		got = in
		return &verbs.SetDogfoodResult{NewSHA: "served"}, nil
	}
	out, err := runCLI(t, "dogfood-deploy", "--task", "T", "--ref", "origin/main", "--compose", "--force", "--lease-override")
	if err != nil || got.Ref != "origin/main" || !got.Compose || !got.Force || !got.LeaseOverride || got.Caller != "cli" || !strings.Contains(out, "served") {
		t.Fatalf("opts=%+v out=%s err=%v", got, out, err)
	}
}
