package verbs

import (
	"strings"
	"testing"
	"time"
)

func TestAbsoluteHoldCannotBeRenewed(t *testing.T) {
	h := newLeaseHarness(t)
	h.cfg.MaxHoldSeconds = 100
	h.claim("A", 0)
	h.live["A"] = Liveness{Class: LiveBusy}
	h.now = h.now.Add(99 * time.Second)
	h.run(func(m *leaseMachine) { m.Renew("A") })
	h.now = h.now.Add(time.Second)
	h.run(func(m *leaseMachine) {
		if m.Renew("A") == nil {
			t.Fatal("renewal revived an expired hold")
		}
	})
	if h.holderID() != "" {
		t.Fatal("expired holder retained without waiters")
	}
}

func TestAbsoluteHoldFlightDeferral(t *testing.T) {
	for _, finish := range []bool{true, false} {
		t.Run(map[bool]string{true: "finish", false: "stale mark"}[finish], func(t *testing.T) {
			h := newLeaseHarness(t)
			h.cfg.MaxHoldSeconds = 100
			h.cfg.InFlightMaxSeconds = 120
			h.claim("A", 0)
			h.claim("B", 0)
			h.run(func(m *leaseMachine) { m.BeginFlight("A") })
			h.advance(100 * time.Second)
			if h.holderID() != "A" {
				t.Fatal("released during valid flight")
			}
			if finish {
				h.run(func(m *leaseMachine) { m.EndFlight("A") })
			} else {
				h.advance(20 * time.Second)
			}
			if h.holderID() != "B" {
				t.Fatalf("holder = %s", h.holderID())
			}
		})
	}
}

func TestEndedHolderReleasedBeforeWarmer(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.live["A"] = Liveness{Class: LiveDead}
	h.advance(time.Second)
	if h.holderID() != "" {
		t.Fatal("dead holder kept until warmer")
	}
}

func TestCoordinatorReservesNextWithoutCommit(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.claim("C", 0)
	h.claim("coord", 1)
	h.wantQueue("coord", "B", "C")
	m := h.m()
	m.env.Contains = func(string) (bool, error) { return true, nil }
	m.Release("A")
	if h.holderID() != "coord" {
		t.Fatal("coordinator slot lost to coalescing")
	}
}

func TestAbsoluteHoldSurvivesRestart(t *testing.T) {
	h := newLeaseHarness(t)
	h.cfg.MaxHoldSeconds = 100
	h.claim("A", 0)
	h.now = h.now.Add(100 * time.Second)
	RecoverLeaseAfterRestart(h.st, h.now, h.cfg)
	h.tick()
	if h.holderID() != "" {
		t.Fatal("restart extended hold")
	}
}

func TestPromptBlockedBusyHolderPassesLease(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.live["A"] = Liveness{Class: LiveSilent} // classifier covers busy + needs_input
	h.claim("B", 0)
	h.advance(30 * time.Second)
	if h.st.Holder.ReleaseAt.IsZero() {
		t.Fatal("no final warning for prompt-blocked holder")
	}
	h.advance(30 * time.Second)
	if h.holderID() != "B" {
		t.Fatal("prompt-blocked holder retained lease")
	}
}

func TestAbsoluteHoldExpiresWithArgusUnreachable(t *testing.T) {
	h := newLeaseHarness(t)
	h.cfg.MaxHoldSeconds = 100
	h.claim("A", 0)
	h.live["A"] = Liveness{Class: LiveUnreachable}
	h.advance(100 * time.Second)
	if h.holderID() != "" {
		t.Fatal("unreachable Argus disabled absolute expiry")
	}
}

func TestGrantNoticeStatesFixedExpiry(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	msgs := h.take()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "2026-01-01T12:30:00Z") || !strings.Contains(msgs[0].Text, "renewal cannot extend") {
		t.Fatalf("grant notice = %+v", msgs)
	}
}

func TestExpiredClaimQueuesBehindReservedSlot(t *testing.T) {
	h := newLeaseHarness(t)
	h.cfg.MaxHoldSeconds = 100
	h.claim("A", 0)
	h.claim("B", 0)
	h.claim("coord", 1)
	h.now = h.now.Add(100 * time.Second)
	out := h.claim("A", 0)
	if out.Granted || out.Position != 2 || h.holderID() != "coord" {
		t.Fatalf("expired claim jumped queue: outcome=%+v holder=%s", out, h.holderID())
	}
	h.wantQueue("B", "A")
}
