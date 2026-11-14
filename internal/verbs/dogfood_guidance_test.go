package verbs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func assertDogfoodReleaseGuidance(t *testing.T, message string) {
	t.Helper()
	for _, want := range []string{
		"Don't hold dogfood for a user who is away.",
		"Don't hold dogfood while fixing a problem you found: call iris_dogfood_release, fix it, then get back in line with a new iris_dogfood_claim.",
		"If the user is away and Iris asks for dogfood back or says someone is waiting, call iris_dogfood_release right away.",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("message missing %q: %s", want, message)
		}
	}
}

func TestDogfoodClaim_ReleaseGuidance(t *testing.T) {
	_, _, _, _, c := leaseFixture(t, "lv-guidance")
	for i := 0; i < 2; i++ {
		result := claim(t, c, "A", 0)
		if result.Status != "granted" {
			t.Fatalf("status = %s", result.Status)
		}
		assertDogfoodReleaseGuidance(t, result.Message)
	}
}

func TestLease_NoticesIncludeReleaseGuidance(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	grant := h.take()
	h.claim("B", 0)
	checkin := h.take()
	h.advance(30 * time.Second)
	final := h.take()
	for _, tc := range []struct {
		kind string
		msgs []LeaseMsg
	}{{"grant", grant}, {"checkin", checkin}, {"final", final}} {
		t.Run(tc.kind, func(t *testing.T) {
			if len(tc.msgs) != 1 || tc.msgs[0].Kind != tc.kind {
				t.Fatalf("messages = %+v", tc.msgs)
			}
			assertDogfoodReleaseGuidance(t, tc.msgs[0].Text)
		})
	}
}

func assertSetDogfoodReleaseGuidance(t *testing.T, result *SetDogfoodResult) {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	var message string
	if err := json.Unmarshal(payload["message"], &message); err != nil {
		t.Fatalf("success result must include a message: %s (%v)", data, err)
	}
	assertDogfoodReleaseGuidance(t, message)
}
