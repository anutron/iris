package verbs

import (
	"fmt"
	"time"

	"github.com/anutron/iris/internal/config"
)

// leaseStateView is shared by status, claim, renewal and advisory task metadata.
// Queue estimates assume no reordering/config edits and bounded flight marks.
func leaseStateView(st *LeaseState, timers config.DogfoodLeaseTimers) *DogfoodLeaseView {
	v := &DogfoodLeaseView{
		Queue: []LeaseQueueView{}, History: []LeaseHistoryEntry{}, Warnings: []string{},
		Message: "Call iris_dogfood_release immediately when testing is done or the user is away. expires_at is the absolute grant deadline; renewals cannot extend it. A valid in-flight deployment may defer release. Queue estimated_by times assume unchanged configuration and order.",
		Timers:  LeaseTimersView{WarmerSeconds: timers.WarmerSeconds, GraceSeconds: timers.GraceSeconds, IdleReleaseSeconds: timers.IdleReleaseSeconds, InFlightMaxSeconds: timers.InFlightMaxSeconds, MaxHoldSeconds: timers.MaxHoldSeconds},
	}
	var next time.Time
	if h := st.Holder; h != nil {
		expires := h.GrantedAt.Add(time.Duration(timers.MaxHoldSeconds) * time.Second)
		hv := &LeaseHolderView{TaskID: h.TaskID, TaskName: h.TaskName, GrantedAt: h.GrantedAt, LastContactAt: h.LastContactAt, ExpiresAt: expires,
			NextCheckinAt: h.LastContactAt.Add(time.Duration(timers.WarmerSeconds) * time.Second), InFlightSetDogfood: h.InFlight > 0}
		if !h.CheckinSentAt.IsZero() {
			t := h.CheckinSentAt
			hv.CheckinOutstandingSince = &t
			if h.CheckinDeliveryState == "pending" {
				v.Warnings = append(v.Warnings, fmt.Sprintf("the check-in to task %s is accepted by argus but not yet submitted to its session (the session is not idle); it appears when the session next goes idle", h.TaskID))
			}
		}
		if !h.ReleaseAt.IsZero() {
			t := h.ReleaseAt
			hv.ReleaseAt = &t
		}
		v.Holder = hv
		idle := h.LastContactAt.Add(time.Duration(timers.IdleReleaseSeconds) * time.Second)
		v.Timers.IdleReleaseAt = &idle
		// Any flight may start just before expiry, including after a current flight.
		next = expires.Add(time.Duration(timers.InFlightMaxSeconds) * time.Second)
	}
	for i, q := range st.Queue {
		entry := LeaseQueueView{TaskID: q.TaskID, TaskName: q.TaskName, Position: i + 1, EnqueuedAt: q.EnqueuedAt}
		if !next.IsZero() {
			t := next
			entry.EstimatedBy = &t
			next = next.Add(time.Duration(timers.MaxHoldSeconds+timers.InFlightMaxSeconds) * time.Second)
		}
		v.Queue = append(v.Queue, entry)
	}
	if st.History != nil {
		v.History = st.History
	}
	return v
}
