package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/barats/xlistman/internal/model"
)

// TestConsoleBounces verifies the owner-only Bounces tab: listing members with
// bounce activity, re-enabling a disabled member (which resets the counter),
// resetting a count, the 409 guard, and the Audit Events recorded.
func TestConsoleBounces(t *testing.T) {
	_, st, baseURL, disc, _ := consoleFixture(t)
	ctx := context.Background()
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)
	base := "/api/console/lists/example.com/dev/bounces"

	// Anonymous: 401. Moderator: 403 (owner-only).
	resp, _ := do(t, baseURL, "GET", base, "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", resp.StatusCode)
	}
	mod := makeSubscriber(t, st, "mod@example.com")
	modCookies := login(t, st, baseURL, mod.Email)
	resp, _ = do(t, baseURL, "GET", base, "", modCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("moderator status = %d, want 403", resp.StatusCode)
	}

	// alice accumulates bounces and is disabled at the threshold (3).
	alice := mustSubscriber(t, st, "alice@example.com")
	subscr, _ := st.GetSubscription(ctx, disc.ID, alice.ID)
	threshold := 3
	settings := disc.Settings
	settings.BounceThreshold = threshold
	if err := st.UpdateListSettings(ctx, disc.ID, settings); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < threshold; i++ {
		if err := st.IncrementBounceCount(ctx, subscr.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetSubscriptionStatus(ctx, subscr.ID, model.SubscriptionStatusDisabled); err != nil {
		t.Fatal(err)
	}

	// Owner sees the bouncing member with count and threshold.
	resp, body := do(t, baseURL, "GET", base, "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("bounces = %d, want 1; %s", len(list), body)
	}
	m := list[0]
	if m["email"] != "alice@example.com" || m["status"] != "disabled" ||
		m["bounce_count"] != float64(threshold) || m["bounce_threshold"] != float64(threshold) {
		t.Errorf("bounce member = %v", m)
	}

	// Re-enable: activates and resets the counter (ADR 0019).
	resp, _ = do(t, baseURL, "POST", base+"/"+fmt.Sprint(alice.ID)+"/re-enable", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-enable status = %d, want 200", resp.StatusCode)
	}
	subscr, _ = st.GetSubscription(ctx, disc.ID, alice.ID)
	if subscr.Status != model.SubscriptionStatusActive || subscr.BounceCount != 0 {
		t.Fatalf("after re-enable: status=%q count=%d, want active/0", subscr.Status, subscr.BounceCount)
	}
	// The re-enabled member is now out of the bounces list.
	resp, body = do(t, baseURL, "GET", base, "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatal("bounces status after re-enable")
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("bounces after re-enable = %d, want 0; %s", len(list), body)
	}

	// Re-enabling a non-disabled member is refused.
	resp, _ = do(t, baseURL, "POST", base+"/"+fmt.Sprint(alice.ID)+"/re-enable", "", ownerCookies)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("re-enable active status = %d, want 409", resp.StatusCode)
	}

	// Reset: alice accumulates bounces below the threshold, appears again, then
	// the owner resets the count (status untouched).
	for i := 0; i < 2; i++ {
		if err := st.IncrementBounceCount(ctx, subscr.ID); err != nil {
			t.Fatal(err)
		}
	}
	resp, body = do(t, baseURL, "GET", base, "", ownerCookies)
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0]["bounce_count"] != float64(2) {
		t.Fatalf("bounces with count 2 = %v", list)
	}
	resp, _ = do(t, baseURL, "POST", base+"/"+fmt.Sprint(alice.ID)+"/reset", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reset status = %d, want 200", resp.StatusCode)
	}
	subscr, _ = st.GetSubscription(ctx, disc.ID, alice.ID)
	if subscr.BounceCount != 0 || subscr.Status != model.SubscriptionStatusActive {
		t.Fatalf("after reset: count=%d status=%q, want 0/active", subscr.BounceCount, subscr.Status)
	}

	// The two privileged actions are audited with the owner as actor.
	events := auditEvents(t, baseURL, "/api/console/lists/example.com/dev/audit", ownerCookies)
	actions := map[string]bool{}
	for _, e := range events {
		actions[fmt.Sprint(e["action"])] = true
	}
	if !actions["member.re-enable"] || !actions["member.reset-bounces"] {
		t.Fatalf("audit actions = %v, want member.re-enable and member.reset-bounces", actions)
	}
	for _, e := range events {
		if e["actor_email"] != "owner@example.com" {
			t.Errorf("audit actor = %v, want owner@example.com", e["actor_email"])
		}
	}
}
