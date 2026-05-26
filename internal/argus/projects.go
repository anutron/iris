package argus

import "context"

// Project is the subset of argus's project record iris consumes.
type Project struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// listProjectsFullResponse mirrors argus's GET /api/projects/full envelope.
type listProjectsFullResponse struct {
	Projects []Project `json:"projects"`
}

// ListProjects fetches argus's full project list. The path field is the
// canonical absolute path to each project's source repo — iris uses it as
// the allowlist for any verb that mutates a source repo.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var resp listProjectsFullResponse
	if _, err := c.doJSON(ctx, "GET", "/api/projects/full", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Projects, nil
}
