package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLeaseDeadlineResultsAndMetadata(t *testing.T) {
	src, _, _, f, client := leaseFixture(t, "lease-deadlines-meta")
	a := claim(t, client, "A", 0)
	if a.Lease == nil || a.Lease.Holder == nil {
		t.Fatal("missing claim snapshot")
	}
	holder := a.Lease.Holder
	expiry := holder.GrantedAt.Add(30 * time.Minute)
	if !holder.ExpiresAt.Equal(expiry) || !strings.Contains(a.Message, expiry.Format(time.RFC3339)) {
		t.Fatalf("claim = %+v", a)
	}
	b := claim(t, client, "B", 0)
	if len(b.Lease.Queue) != 1 || b.Lease.Queue[0].Position != 1 || b.Lease.Queue[0].EstimatedBy == nil || !b.Lease.Queue[0].EstimatedBy.Equal(expiry.Add(15*time.Minute)) {
		t.Fatalf("queue = %+v", b.Lease.Queue)
	}
	r, err := DogfoodRenew(t.Context(), client, "A")
	if err != nil || !r.Lease.Holder.ExpiresAt.Equal(expiry) || r.Lease.Holder.LastContactAt.Before(holder.LastContactAt) {
		t.Fatalf("renew = %+v, %v", r, err)
	}
	v := ReadDogfoodLeaseView(t.Context(), canonicalize(src))
	if !v.Holder.ExpiresAt.Equal(expiry) || v.Timers.MaxHoldSeconds != 1800 || !strings.Contains(v.Message, "cannot extend") {
		t.Fatalf("status = %+v", v)
	}
	f.mu.Lock()
	var meta struct {
		Holder   *LeaseHolderView
		Position int
		Queue    []LeaseQueueView
	}
	err = json.Unmarshal([]byte(f.meta["B"]), &meta)
	f.mu.Unlock()
	if err != nil || meta.Position != 1 || meta.Holder == nil || meta.Holder.TaskID != "A" || !meta.Holder.ExpiresAt.Equal(expiry) {
		t.Fatalf("meta = %+v, %v", meta, err)
	}
	if _, err := DogfoodRelease(t.Context(), client, "A"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.meta["A"] != "" {
		t.Fatal("departed holder metadata not cleared")
	}
	if err := json.Unmarshal([]byte(f.meta["B"]), &meta); err != nil || meta.Holder.TaskID != "B" || meta.Position != 0 {
		t.Fatalf("handoff metadata = %+v, %v", meta, err)
	}
}

func TestLeaseMetadataFailureIsWarning(t *testing.T) {
	_, _, _, f, client := leaseFixture(t, "lease-meta-error")
	f.metaHTTP = http.StatusForbidden
	r := claim(t, client, "A", 0)
	if r.Status != "granted" || len(r.Warnings) == 0 || !strings.Contains(strings.Join(r.Warnings, " "), "metadata") {
		t.Fatalf("result = %+v", r)
	}
}

func TestValidateConfigMaxHold(t *testing.T) {
	for _, n := range []string{"0", "-1", "1200"} {
		t.Run(n, func(t *testing.T) {
			src, client := validateFixture(t, validateLeaseBase)
			if err := os.WriteFile(filepath.Join(src, ".iris.local.toml"), []byte("[dogfood_lease]\nmax_hold_seconds = "+n+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			r, err := ValidateConfig(context.Background(), client, ValidateConfigInput{Path: src})
			if err != nil {
				t.Fatal(err)
			}
			if n == "1200" {
				if !r.Valid || r.Resolved.DogfoodLeaseTimers().MaxHoldSeconds != 1200 {
					t.Fatalf("resolved = %+v", r)
				}
				return
			}
			for _, e := range r.Errors {
				if e.Field == "dogfood_lease.max_hold_seconds" && !r.Valid {
					return
				}
			}
			t.Fatalf("missing validation error: %+v", r)
		})
	}
}

func TestLeaseTickReleasesEndedTaskThroughAPI(t *testing.T) {
	for _, end := range []string{"complete", "archive", "stop"} {
		t.Run(end, func(t *testing.T) {
			src, _, _, f, c := leaseFixture(t, "ended-"+end)
			claim(t, c, "A", 0)
			f.with(func() {
				switch end {
				case "complete":
					f.tasks["A"].Status = "complete"
				case "archive":
					f.tasks["A"].Archived = true
				case "stop":
					stopped := false
					f.tasks["A"].Live = &stopped
					f.tasks["A"].Bridge = "none"
				}
			})
			if err := TickLease(t.Context(), c, canonicalize(src)); err != nil {
				t.Fatal(err)
			}
			st, err := loadLease(canonicalize(src))
			if err != nil || st.Holder != nil {
				t.Fatalf("ended task retained: %+v, %v", st, err)
			}
		})
	}
}

func TestExpiredHolderCannotEnterDeployGate(t *testing.T) {
	src, _, _, _, c := leaseFixture(t, "expired-gate")
	claim(t, c, "A", 0)
	claim(t, c, "B", 0)
	canon := canonicalize(src)
	st, err := loadLease(canon)
	if err != nil {
		t.Fatal(err)
	}
	st.Holder.GrantedAt = time.Now().Add(-30 * time.Minute)
	if err := saveLease(canon, st); err != nil {
		t.Fatal(err)
	}
	lr, err := resolveLeaseRepo(t.Context(), canon)
	if err != nil {
		t.Fatal(err)
	}
	_, err = enterLeaseGate(t.Context(), c, lr, "A", false, false)
	var refusal *LeaseGateError
	if !errors.As(err, &refusal) || refusal.Holder == nil || refusal.Holder.TaskID != "B" {
		t.Fatalf("expired holder entered gate: %v", err)
	}
	st, err = loadLease(canon)
	if err != nil || st.Holder.TaskID != "B" || st.Holder.InFlight != 0 {
		t.Fatalf("bad handoff: %+v, %v", st, err)
	}
}
