package argus

import (
	"context"
	"net/url"
	"time"
)

// Notify delivers text to a task's session as a user turn via argus pane
// delivery (POST /api/tasks/{id}/notify, submit=true). Argus holds the
// delivery pending until the session is idle, up to deadline. deliveryID must
// match [A-Za-z0-9_-]{1,128}. A 404 means the task is gone (IsNotFound).
// The returned state is argus's view of the delivery right after the post:
// "submitted" (typed into the session) or "pending" (waiting for idle).
func (c *Client) Notify(ctx context.Context, taskID, text, deliveryID string, deadline time.Duration) (string, error) {
	body := map[string]any{
		"text":        text,
		"submit":      true,
		"delivery_id": deliveryID,
		"deadline_ms": deadline.Milliseconds(),
	}
	var resp struct {
		State string `json:"state"`
	}
	if _, err := c.doJSON(ctx, "POST", "/api/tasks/"+url.PathEscape(taskID)+"/notify", body, &resp); err != nil {
		return "", err
	}
	return resp.State, nil
}

// CancelNotify cancels a pending delivery (DELETE /api/tasks/{id}/notify/{delivery_id}).
func (c *Client) CancelNotify(ctx context.Context, taskID, deliveryID string) error {
	_, err := c.doJSON(ctx, "DELETE", "/api/tasks/"+url.PathEscape(taskID)+"/notify/"+url.PathEscape(deliveryID), nil, nil)
	return err
}
