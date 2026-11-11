package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/verbs"
)

// leaseErr renders a lease-verb error. Refusals (gate, renew) carry the
// holder/ahead/position as structured fields on a second content block.
func leaseErr(verb string, err error) Response {
	var ge *verbs.LeaseGateError
	if errors.As(err, &ge) {
		return ErrorResponseWithFields(fmt.Sprintf("%s: %v", verb, err), map[string]any{
			"holder": ge.Holder, "ahead": ge.Ahead, "position": ge.Position,
		})
	}
	var ref *verbs.LeaseRefusal
	if errors.As(err, &ref) {
		return ErrorResponseWithFields(fmt.Sprintf("%s: %v", verb, err), ref)
	}
	return ErrorResponse(fmt.Sprintf("%s: %v", verb, err))
}

func leaseJSON(verb string, v any) Response {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ErrorResponse(fmt.Sprintf("%s: encode result: %v", verb, err))
	}
	return TextResponse(string(body))
}

func decodeInput(verb string, input json.RawMessage, into any) *Response {
	if len(input) > 0 {
		if err := json.Unmarshal(input, into); err != nil {
			r := ErrorResponse(fmt.Sprintf("%s: invalid input: %v", verb, err))
			return &r
		}
	}
	return nil
}

type dogfoodClaimInput struct {
	TaskID      string `json:"task_id"`
	WaitSeconds int    `json:"wait_seconds,omitempty"`
	Position    int    `json:"position,omitempty"`
}

// NewDogfoodClaimHandler returns a Handler for iris_dogfood_claim.
func NewDogfoodClaimHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in dogfoodClaimInput
		if r := decodeInput("iris:dogfood_claim", input, &in); r != nil {
			return *r
		}
		res, err := verbs.DogfoodClaim(ctx, client, verbs.DogfoodClaimInput{TaskID: in.TaskID, WaitSeconds: in.WaitSeconds, Position: in.Position})
		if err != nil {
			return leaseErr("iris:dogfood_claim", err)
		}
		return leaseJSON("iris:dogfood_claim", res)
	})
}

type dogfoodTaskInput struct {
	TaskID string `json:"task_id"`
}

// NewDogfoodRenewHandler returns a Handler for iris_dogfood_renew.
func NewDogfoodRenewHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in dogfoodTaskInput
		if r := decodeInput("iris:dogfood_renew", input, &in); r != nil {
			return *r
		}
		res, err := verbs.DogfoodRenew(ctx, client, in.TaskID)
		if err != nil {
			return leaseErr("iris:dogfood_renew", err)
		}
		return leaseJSON("iris:dogfood_renew", res)
	})
}

// NewDogfoodReleaseHandler returns a Handler for iris_dogfood_release.
func NewDogfoodReleaseHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in dogfoodTaskInput
		if r := decodeInput("iris:dogfood_release", input, &in); r != nil {
			return *r
		}
		res, err := verbs.DogfoodRelease(ctx, client, in.TaskID)
		if err != nil {
			return leaseErr("iris:dogfood_release", err)
		}
		return leaseJSON("iris:dogfood_release", res)
	})
}

type dogfoodForceReleaseInput struct {
	TaskID string `json:"task_id,omitempty"`
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason"`
}

// NewDogfoodForceReleaseHandler returns a Handler for iris_dogfood_force_release.
func NewDogfoodForceReleaseHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in dogfoodForceReleaseInput
		if r := decodeInput("iris:dogfood_force_release", input, &in); r != nil {
			return *r
		}
		res, err := verbs.DogfoodForceRelease(ctx, client, verbs.DogfoodForceReleaseInput{TaskID: in.TaskID, Path: in.Path, Reason: in.Reason})
		if err != nil {
			return leaseErr("iris:dogfood_force_release", err)
		}
		return leaseJSON("iris:dogfood_force_release", res)
	})
}

type dogfoodQueueMoveInput struct {
	TaskID     string `json:"task_id,omitempty"`
	Path       string `json:"path,omitempty"`
	MoveTaskID string `json:"move_task_id"`
	Position   int    `json:"position"`
}

// NewDogfoodQueueMoveHandler returns a Handler for iris_dogfood_queue_move.
func NewDogfoodQueueMoveHandler(client *argus.Client) Handler {
	return HandlerFunc(func(ctx context.Context, input json.RawMessage) Response {
		var in dogfoodQueueMoveInput
		if r := decodeInput("iris:dogfood_queue_move", input, &in); r != nil {
			return *r
		}
		res, err := verbs.DogfoodQueueMove(ctx, client, verbs.DogfoodQueueMoveInput{TaskID: in.TaskID, Path: in.Path, MoveTaskID: in.MoveTaskID, Position: in.Position})
		if err != nil {
			return leaseErr("iris:dogfood_queue_move", err)
		}
		return leaseJSON("iris:dogfood_queue_move", res)
	})
}
