package verbs

import (
	"context"

	"github.com/anutron/iris/internal/argus"
)

// LivenessClass is iris's classification of a leased/queued argus task (design D5).
type LivenessClass string

const (
	LiveDead        LivenessClass = "dead"
	LiveBusy        LivenessClass = "busy"
	LiveSilent      LivenessClass = "silent"
	LiveNoBridge    LivenessClass = "unknown-no-bridge"
	LiveUnreachable LivenessClass = "unknown-unreachable"
)

// Liveness is a classification plus what the lookup learned.
type Liveness struct {
	Class    LivenessClass
	TaskName string // argus task name when known
	Err      string // lookup error text for unreachable
}

// classifyLiveness maps argus's answers onto the D5 table. Pure.
func classifyLiveness(task *argus.Task, taskErr error, bridge *argus.Bridge, bridgeErr error) Liveness {
	if taskErr != nil {
		if argus.IsNotFound(taskErr) {
			return Liveness{Class: LiveDead}
		}
		return Liveness{Class: LiveUnreachable, Err: taskErr.Error()}
	}
	lv := Liveness{}
	if task != nil {
		lv.TaskName = task.Name
		if task.Status == "complete" || task.Archived {
			lv.Class = LiveDead
			return lv
		}
	}
	if bridgeErr != nil {
		if bridgeErr == argus.ErrNoBridge {
			lv.Class = LiveNoBridge
			return lv
		}
		lv.Class = LiveUnreachable
		lv.Err = bridgeErr.Error()
		return lv
	}
	switch {
	case bridge == nil:
		lv.Class = LiveNoBridge
	case !bridge.Live:
		lv.Class = LiveDead
	case bridge.Busy:
		lv.Class = LiveBusy
	default: // needs_input, or simply quiet: normal silence rules
		lv.Class = LiveSilent
	}
	return lv
}

// lookupLiveness asks argus about taskID and classifies the answer.
func lookupLiveness(ctx context.Context, client *argus.Client, taskID string) Liveness {
	task, terr := client.GetTask(ctx, taskID)
	if terr != nil {
		return classifyLiveness(nil, terr, nil, nil)
	}
	if task.Status == "complete" || task.Archived {
		return classifyLiveness(task, nil, nil, nil)
	}
	bridge, berr := client.GetBridge(ctx, taskID)
	return classifyLiveness(task, nil, bridge, berr)
}
