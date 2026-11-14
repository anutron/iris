package argus

import (
	"context"
	"net/url"
)

// SetLeaseMeta writes advisory lease state in Iris's plugin-owned namespace.
// Argus scopes plugin tokens to that namespace. An empty value clears a summary.
func (c *Client) SetLeaseMeta(ctx context.Context, taskID, repoKey, summary string) error {
	_, err := c.doJSON(ctx, "PUT", "/api/tasks/"+url.PathEscape(taskID)+"/meta", map[string]string{
		"namespace": "iris", "key": "dogfood_lease_" + repoKey, "value": summary,
	}, nil)
	return err
}
