package argus

import (
	"errors"
	"sync"
	"testing"
)

func TestLinkState_StringValues(t *testing.T) {
	cases := map[LinkState]string{
		LinkHealthy:    "healthy",
		LinkRecovering: "recovering",
		LinkDown:       "down",
		LinkState(99):  "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("LinkState(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestLinkState_GetSetRoundTrip(t *testing.T) {
	// Restore baseline so this test doesn't bleed into siblings.
	t.Cleanup(func() {
		SetLinkState(LinkHealthy)
		SetLinkError(nil)
	})

	for _, s := range []LinkState{LinkHealthy, LinkRecovering, LinkDown} {
		SetLinkState(s)
		if got := GetLinkState(); got != s {
			t.Errorf("GetLinkState after Set(%v) = %v", s, got)
		}
	}
}

func TestLinkError_StoreAndClear(t *testing.T) {
	t.Cleanup(func() { SetLinkError(nil) })

	SetLinkError(nil)
	if LinkLastError() != nil {
		t.Fatalf("expected nil after clear, got %v", LinkLastError())
	}

	want := errors.New("ports query failed")
	SetLinkError(want)
	if got := LinkLastError(); got != want {
		t.Fatalf("LinkLastError = %v, want %v", got, want)
	}

	SetLinkError(nil)
	if LinkLastError() != nil {
		t.Fatalf("expected nil after re-clear, got %v", LinkLastError())
	}
}

// Race-detector smoke: concurrent SetLinkState / GetLinkState / SetLinkError /
// LinkLastError must not data-race under `-race`.
func TestLinkState_ConcurrentSafe(t *testing.T) {
	t.Cleanup(func() {
		SetLinkState(LinkHealthy)
		SetLinkError(nil)
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				SetLinkState(LinkRecovering)
				_ = GetLinkState()
				SetLinkError(errors.New("oops"))
				_ = LinkLastError()
				SetLinkState(LinkHealthy)
				SetLinkError(nil)
			}
		}()
	}
	wg.Wait()
}
