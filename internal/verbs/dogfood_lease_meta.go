package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// stampLeaseMeta is advisory: never change lease state based on a write failure.
func stampLeaseMeta(ctx context.Context, client *argus.Client, lr *leaseRepo, before []byte, st *LeaseState) []string {
	if client == nil {
		return nil
	}
	var old LeaseState
	_ = json.Unmarshal(before, &old)
	ids := map[string]bool{}
	for _, s := range []*LeaseState{&old, st} {
		if s.Holder != nil {
			ids[s.Holder.TaskID] = true
		}
		for _, q := range s.Queue {
			ids[q.TaskID] = true
		}
	}
	v := leaseStateView(st, lr.Timers)
	var warnings []string
	mctx, cancel := context.WithTimeout(ctx, leaseArgusTimeout)
	defer cancel()
	for id := range ids {
		var summary string
		if leaseNames(st, id) {
			position := 0
			var estimatedBy *time.Time
			for _, q := range v.Queue {
				if q.TaskID == id {
					position = q.Position
					estimatedBy = q.EstimatedBy
				}
			}
			// Keep metadata compact; full history and diagnostics remain in status.
			body, _ := json.Marshal(struct {
				SourceRepo  string           `json:"source_repo"`
				Holder      *LeaseHolderView `json:"holder"`
				Position    int              `json:"position"`
				QueueLength int              `json:"queue_length"`
				EstimatedBy *time.Time       `json:"estimated_by,omitempty"`
				UpdatedAt   time.Time        `json:"updated_at"`
			}{lr.SourceRepo, v.Holder, position, len(v.Queue), estimatedBy, time.Now().UTC()})
			summary = string(body)
		}
		err := client.SetLeaseMeta(mctx, id, lr.Slug, summary)
		if err != nil && !argus.IsNotFound(err) {
			warnings = append(warnings, fmt.Sprintf("could not stamp lease metadata for task %s: %v", id, err))
		}
	}
	return warnings
}
