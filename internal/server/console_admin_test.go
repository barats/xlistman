package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/barats/xlistman/internal/model"
)

// fullSettingsJSON is a complete ListSettings payload matching the shape the
// console Settings form sends back on PUT (full replace).
const fullSettingsJSON = `{"moderation_enabled":true,"subject_prefix":"[dev]","footer_enabled":true,"max_message_size":1000000,"archive_max_age_days":0,"digest_frequency":"weekly","subscription_policy":"moderated","reply_to_mode":"list","reply_to_address":"","welcome_email":true,"goodbye_email":true,"sender_held_notice":true,"owner_auto_disable_notice":false,"bounce_threshold":5,"held_expiry_days":7}`

func TestConsoleSettings(t *testing.T) {
	_, st, baseURL, _, _ := consoleFixture(t)
	ctx := context.Background()
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)
	base := "/api/console/lists/example.com/dev/settings"

	// Owner reads settings.
	resp, body := do(t, baseURL, "GET", base, "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get settings status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"description"`) || !strings.Contains(body, `"settings"`) {
		t.Errorf("settings response missing fields: %s", body)
	}

	// Owner updates settings and description.
	put := `{"description":"Dev discussion","settings":` + fullSettingsJSON + `}`
	resp, body = do(t, baseURL, "PUT", base, put, ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put settings status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	l, _ := st.GetList(ctx, "dev", "example.com")
	if l.Description != "Dev discussion" {
		t.Errorf("description = %q, want updated", l.Description)
	}
	if l.Settings.SubjectPrefix != "[dev]" || l.Settings.DigestFrequency != model.DigestWeekly ||
		l.Settings.SubscriptionPolicy != model.SubscriptionPolicyModerated {
		t.Errorf("settings not updated: %+v", l.Settings)
	}

	// Validation: specified reply-to without an address is rejected.
	bad := `{"description":"","settings":{"moderation_enabled":false,"subject_prefix":"","footer_enabled":true,"max_message_size":1000000,"archive_max_age_days":0,"digest_frequency":"daily","subscription_policy":"open","reply_to_mode":"specified","reply_to_address":"","welcome_email":true,"goodbye_email":true,"sender_held_notice":true,"owner_auto_disable_notice":false,"bounce_threshold":5,"held_expiry_days":14}}`
	resp, _ = do(t, baseURL, "PUT", base, bad, ownerCookies)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid settings status = %d, want 400", resp.StatusCode)
	}

	// Moderator cannot read or edit settings.
	mod := makeSubscriber(t, st, "mod@example.com")
	modCookies := login(t, st, baseURL, mod.Email)
	resp, _ = do(t, baseURL, "GET", base, "", modCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("moderator get settings status = %d, want 403", resp.StatusCode)
	}
}

func TestConsoleMembers(t *testing.T) {
	_, st, baseURL, disc, _ := consoleFixture(t)
	ctx := context.Background()
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)
	base := "/api/console/lists/example.com/dev/members"

	// Owner lists members: alice is an active member with no roles.
	resp, body := do(t, baseURL, "GET", base, "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get members status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "alice@example.com") {
		t.Errorf("member list missing alice: %s", body)
	}
	// Roles must be a JSON array (never null) so the frontend can iterate.
	var got []map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal members: %v", err)
	}
	for _, m := range got {
		roles, ok := m["roles"]
		if !ok {
			t.Errorf("member %v missing roles field", m["email"])
			continue
		}
		if arr, ok := roles.([]any); !ok || arr == nil {
			t.Errorf("member %v roles = %#v, want a JSON array", m["email"], roles)
		}
	}

	// Add a new member authoritatively (no double opt-in).
	resp, body = do(t, baseURL, "POST", base, `{"email":"bob@example.com"}`, ownerCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status = %d, want 201; body=%s", resp.StatusCode, body)
	}
	bob := mustSubscriber(t, st, "bob@example.com")
	subscr, err := st.GetSubscription(ctx, disc.ID, bob.ID)
	if err != nil {
		t.Fatalf("bob not subscribed after add: %v", err)
	}
	if subscr.Status != model.SubscriptionStatusActive {
		t.Errorf("bob status = %q, want active", subscr.Status)
	}

	// Adding an existing member errors.
	resp, body = do(t, baseURL, "POST", base, `{"email":"bob@example.com"}`, ownerCookies)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("duplicate add status = %d, want 400; body=%s", resp.StatusCode, body)
	}

	// Remove bob.
	resp, _ = do(t, baseURL, "DELETE", base+"/"+fmt.Sprint(bob.ID), "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove member status = %d, want 200", resp.StatusCode)
	}
	if _, err := st.GetSubscription(ctx, disc.ID, bob.ID); err == nil {
		t.Errorf("bob still subscribed after remove")
	}

	// Moderator cannot view members.
	mod := makeSubscriber(t, st, "mod@example.com")
	modCookies := login(t, st, baseURL, mod.Email)
	resp, _ = do(t, baseURL, "GET", base, "", modCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("moderator members status = %d, want 403", resp.StatusCode)
	}
}

func TestConsoleHeldSubscriptionActions(t *testing.T) {
	_, st, baseURL, disc, _ := consoleFixture(t)
	ctx := context.Background()
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)

	heldSub := func(email string) *model.Subscriber {
		t.Helper()
		sub := makeSubscriber(t, st, email)
		subscr, err := st.CreateSubscription(ctx, disc.ID, sub.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.ConfirmSubscription(ctx, subscr.ID, model.SubscriptionStatusHeld); err != nil {
			t.Fatal(err)
		}
		return sub
	}

	// Approve a held subscription: activates it and queues the welcome email.
	carol := heldSub("carol@example.com")
	resp, body := do(t, baseURL, "POST", "/api/console/lists/example.com/dev/members/"+fmt.Sprint(carol.ID)+"/approve", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	subscr, _ := st.GetSubscription(ctx, disc.ID, carol.ID)
	if subscr.Status != model.SubscriptionStatusActive {
		t.Errorf("carol status = %q, want active", subscr.Status)
	}
	if to := strings.Join(queuedTo(t, st, ctx), ","); !strings.Contains(to, "carol@example.com") {
		t.Errorf("welcome not queued to carol; got %q", to)
	}

	// Reject a held subscription: removes it.
	dave := heldSub("dave@example.com")
	resp, _ = do(t, baseURL, "POST", "/api/console/lists/example.com/dev/members/"+fmt.Sprint(dave.ID)+"/reject", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reject status = %d, want 200", resp.StatusCode)
	}
	if _, err := st.GetSubscription(ctx, disc.ID, dave.ID); err == nil {
		t.Errorf("dave subscription still exists after reject")
	}

	// Approving a non-held subscription errors.
	erin := makeSubscriber(t, st, "erin@example.com")
	resp, _ = do(t, baseURL, "POST", "/api/console/lists/example.com/dev/members/"+fmt.Sprint(erin.ID)+"/approve", "", ownerCookies)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("approve non-held status = %d, want 400", resp.StatusCode)
	}

	// Moderator cannot approve subscriptions.
	mod := makeSubscriber(t, st, "mod@example.com")
	modCookies := login(t, st, baseURL, mod.Email)
	resp, _ = do(t, baseURL, "POST", "/api/console/lists/example.com/dev/members/"+fmt.Sprint(carol.ID)+"/approve", "", modCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("moderator approve status = %d, want 403", resp.StatusCode)
	}
}

func TestConsoleRoles(t *testing.T) {
	_, st, baseURL, disc, _ := consoleFixture(t)
	ctx := context.Background()
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)
	alice := mustSubscriber(t, st, "alice@example.com")
	base := "/api/console/lists/example.com/dev/roles/"

	// Grant moderator to alice, then revoke it.
	resp, body := do(t, baseURL, "POST", base+fmt.Sprint(alice.ID)+"/moderator", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("grant moderator status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	if ok, _ := st.IsModerator(ctx, disc.ID, alice.ID); !ok {
		t.Errorf("alice not moderator after grant")
	}
	resp, _ = do(t, baseURL, "DELETE", base+fmt.Sprint(alice.ID)+"/moderator", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke moderator status = %d, want 200", resp.StatusCode)
	}
	if ok, _ := st.IsModerator(ctx, disc.ID, alice.ID); ok {
		t.Errorf("alice still moderator after revoke")
	}

	// Grant owner to alice; the original owner can revoke their own role
	// because a second owner exists.
	resp, _ = do(t, baseURL, "POST", base+fmt.Sprint(alice.ID)+"/owner", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("grant owner status = %d, want 200", resp.StatusCode)
	}
	resp, body = do(t, baseURL, "DELETE", base+fmt.Sprint(owner.ID)+"/owner", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke non-last owner status = %d, want 200; body=%s", resp.StatusCode, body)
	}

	// alice is now the only owner; revoking her own owner role is refused.
	aliceCookies := login(t, st, baseURL, alice.Email)
	resp, body = do(t, baseURL, "DELETE", base+fmt.Sprint(alice.ID)+"/owner", "", aliceCookies)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("last-owner revoke status = %d, want 409; body=%s", resp.StatusCode, body)
	}
	if ok, _ := st.IsOwner(ctx, disc.ID, alice.ID); !ok {
		t.Errorf("alice no longer owner after refused revoke")
	}

	// Moderator cannot manage roles.
	mod := makeSubscriber(t, st, "mod@example.com")
	modCookies := login(t, st, baseURL, mod.Email)
	resp, _ = do(t, baseURL, "POST", base+fmt.Sprint(mod.ID)+"/moderator", "", modCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("moderator grant status = %d, want 403", resp.StatusCode)
	}

	// Unknown role is not found.
	resp, _ = do(t, baseURL, "POST", base+fmt.Sprint(alice.ID)+"/boss", "", aliceCookies)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown role status = %d, want 404", resp.StatusCode)
	}
}

// TestConsoleListInfoRoleGate confirms a Moderator can read the list-info
// route (which powers the console tab bar) even though they cannot touch the
// owner-only admin sections.
func TestConsoleListInfoRoleGate(t *testing.T) {
	_, st, baseURL, _, _ := consoleFixture(t)
	mod := makeSubscriber(t, st, "mod@example.com")
	modCookies := login(t, st, baseURL, mod.Email)

	resp, body := do(t, baseURL, "GET", "/api/console/lists/example.com/dev", "", modCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("moderator list-info status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"moderator"`) {
		t.Errorf("list-info roles missing moderator for moderator: %s", body)
	}
}

// TestConsoleSettingsTypeChangeNotInWeb confirms the web console cannot change
// a list's type (ADR 0016 keeps structural changes CLI-only).
func TestConsoleSettingsTypeChangeNotInWeb(t *testing.T) {
	_, st, baseURL, _, _ := consoleFixture(t)
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)
	base := "/api/console/lists/example.com/dev/settings"

	put := `{"description":"x","list_type":"newsletter","settings":` + fullSettingsJSON + `}`
	resp, body := do(t, baseURL, "PUT", base, put, ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put settings status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	l, err := st.GetList(context.Background(), "dev", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if l.ListType != model.ListTypeDiscussion {
		t.Errorf("list type changed via web: %q, want discussion", l.ListType)
	}
}
