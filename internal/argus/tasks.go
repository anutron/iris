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

// listTasksResponse mirrors argus's GET /api/tasks envelope, modelled on
// the same `{ "<resource>": [...] }` shape used by /api/projects/full.
type listTasksResponse struct {
	Tasks []Task `json:"tasks"`
}

// GetTask fetches one task by ID.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	var t Task
	if _, err := c.doJSON(ctx, "GET", "/api/tasks/"+url.PathEscape(taskID), nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTasks fetches argus's full task list. Iris filters client-side to
// find a task whose worktree_path matches a given source repo; argus's
// task list is small enough that server-side filtering is unnecessary.
func (c *Client) ListTasks(ctx context.Context) ([]Task, error) {
	var resp listTasksResponse
	if _, err := c.doJSON(ctx, "GET", "/api/tasks", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// SetTaskStatus updates an argus task's status (e.g., "complete").
func (c *Client) SetTaskStatus(ctx context.Context, taskID, status string) error {
	body := map[string]string{"status": status}
	_, err := c.doJSON(ctx, "POST", "/api/tasks/"+url.PathEscape(taskID)+"/status", body, nil)
	return err
}

// ArchiveTask asks argus to archive the task. Argus performs server-side
// cleanup (e.g., removing the worktree).
func (c *Client) ArchiveTask(ctx context.Context, taskID string) error {
	_, err := c.doJSON(ctx, "POST", "/api/tasks/"+url.PathEscape(taskID)+"/archive", nil, nil)
	return err
}
