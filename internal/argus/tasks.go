package argus

import (
	"context"
	"net/url"
)

// Task is the subset of argus's task representation iris consumes.
// Argus may return additional fields; they are ignored.
type Task struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Project      string `json:"project"`
	Status       string `json:"status"`
	WorktreePath string `json:"worktree_path"`
	Branch       string `json:"branch"`
}

// GetTask fetches one task by ID.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	var t Task
	if _, err := c.doJSON(ctx, "GET", "/api/tasks/"+url.PathEscape(taskID), nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
