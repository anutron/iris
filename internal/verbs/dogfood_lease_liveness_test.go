package verbs

import (
	"errors"
	"testing"

	"github.com/anutron/iris/internal/argus"
)

// TestClassifyLiveness covers every row of design D5.
func TestClassifyLiveness(t *testing.T) {
	notFound := &argus.HTTPError{Method: "GET", Path: "/x", StatusCode: 404}
	unavailable := &argus.HTTPError{Method: "GET", Path: "/x", StatusCode: 503}
	live := func(busy, needs bool) *argus.Bridge { return &argus.Bridge{Live: true, Busy: busy, NeedsInput: needs} }
	task := func(status string, archived bool) *argus.Task {
		return &argus.Task{ID: "t", Name: "nm", Status: status, Archived: archived}
	}
	cases := []struct {
		name      string
		task      *argus.Task
		taskErr   error
		bridge    *argus.Bridge
		bridgeErr error
		want      LivenessClass
	}{
		{"task 404", nil, notFound, nil, nil, LiveDead},
		{"complete", task("complete", false), nil, live(false, false), nil, LiveDead},
		{"archived", task("in_progress", true), nil, live(true, false), nil, LiveDead},
		{"bridge not live", task("in_progress", false), nil, &argus.Bridge{Live: false}, nil, LiveDead},
		{"busy", task("in_progress", false), nil, live(true, false), nil, LiveBusy},
		{"needs input", task("in_progress", false), nil, live(false, true), nil, LiveSilent},
		{"idle live", task("in_progress", false), nil, live(false, false), nil, LiveSilent},
		{"busy and needs input is busy", task("in_progress", false), nil, live(true, true), nil, LiveBusy},
		{"no bridge", task("in_progress", false), nil, nil, argus.ErrNoBridge, LiveNoBridge},
		{"no bridge but complete", task("complete", false), nil, nil, argus.ErrNoBridge, LiveDead},
		{"task 5xx", nil, unavailable, nil, nil, LiveUnreachable},
		{"task network error", nil, errors.New("dial tcp: refused"), nil, nil, LiveUnreachable},
		{"bridge 5xx", task("in_progress", false), nil, nil, unavailable, LiveUnreachable},
		{"bridge network error", task("in_progress", false), nil, nil, errors.New("eof"), LiveUnreachable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLiveness(tc.task, tc.taskErr, tc.bridge, tc.bridgeErr)
			if got.Class != tc.want {
				t.Fatalf("class = %v, want %v", got.Class, tc.want)
			}
			if tc.task != nil && got.TaskName != "nm" {
				t.Fatalf("task name not carried: %+v", got)
			}
			if tc.want == LiveUnreachable && got.Err == "" {
				t.Fatal("unreachable must carry an error string")
			}
		})
	}
}
