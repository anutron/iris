package argus

import (
	"context"
	"errors"
	"net/url"
	"time"
)

// ErrNoBridge is returned by GetBridge when argus has no bridge state for the
// task (404): a non-Claude backend, or a bridge that never started.
var ErrNoBridge = errors.New("argus: no bridge state for task")

// Bridge is the subset of GET /api/tasks/{id}/bridge iris consumes.
type Bridge struct {
	Live            bool      `json:"live"`
	Busy            bool      `json:"busy"`
	NeedsInput      bool      `json:"needs_input"`
	LastEventAt     time.Time `json:"last_event_at"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at"`
}

// GetBridge fetches a task's bridge state. A 404 maps to ErrNoBridge.
func (c *Client) GetBridge(ctx context.Context, taskID string) (*Bridge, error) {
	var b Bridge
	if _, err := c.doJSON(ctx, "GET", "/api/tasks/"+url.PathEscape(taskID)+"/bridge", nil, &b); err != nil {
		if IsNotFound(err) {
			return nil, ErrNoBridge
		}
		return nil, err
	}
	return &b, nil
}
