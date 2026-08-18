package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// auditEvents fetches the audit trail for a list (or instance-wide when base
// is the admin audit route) and returns the parsed events.
func auditEvents(t *testing.T, baseURL, path string, cookies []*http.Cookie) []map[string]any {
	t.Helper()
	resp, body := do(t, baseURL, "GET", path, "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit %s status = %d, want 200; body=%s", path, resp.StatusCode, body)
	}
	var events []map[string]any
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		t.Fatalf("unmarshal audit: %v; body=%s", err, body)
	}
	return events
}

func TestConsoleAuditGateAndSettingsRecording(t *testing.T) {
	_, st, baseURL, _, _ := consoleFixture(t)
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)
	base := "/api/console/lists/example.com/dev/audit"

	// Anonymous: 401. Moderator: 403 (owner-only per ADR 0018).
	resp, _ := do(t, baseURL, "GET", base, "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous audit status = %d, want 401", resp.StatusCode)
	}
	mod := makeSubscriber(t, st, "mod@example.com")
	modCookies := login(t, st, baseURL, mod.Email)
	resp, _ = do(t, baseURL, "GET", base, "", modCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("moderator audit status = %d, want 403", resp.StatusCode)
	}

	// Owner saves settings on dev: records a settings.update event.
	put := `{"description":"Dev list","settings":` + fullSettingsJSON + `}`
	resp, body := do(t, baseURL, "PUT", "/api/console/lists/example.com/dev/settings", put, ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put settings status = %d; body=%s", resp.StatusCode, body)
	}

	// A settings update on the other (newsletter) list must not leak into dev's
	// per-list audit trail.
	newsPut := `{"description":"Newsletter","settings":` + fullSettingsJSON + `}`
	if resp, _ = do(t, baseURL, "PUT", "/api/console/lists/example.com/news/settings", newsPut, ownerCookies); resp.StatusCode != http.StatusOK {
		t.Fatalf("put news settings status = %d", resp.StatusCode)
	}

	events := auditEvents(t, baseURL, base, ownerCookies)
	if len(events) != 1 {
		t.Fatalf("dev audit events = %d, want 1; got %v", len(events), events)
	}
	e := events[0]
	if e["action"] != "settings.update" {
		t.Errorf("action = %v, want settings.update", e["action"])
	}
	if e["actor_kind"] != "subscriber" || e["actor_email"] != "owner@example.com" {
		t.Errorf("actor = %v / %v, want subscriber owner@example.com", e["actor_kind"], e["actor_email"])
	}
	if e["list_addr"] != "dev@example.com" {
		t.Errorf("list_addr = %v, want dev@example.com", e["list_addr"])
	}
	if !strings.Contains(fmt.Sprint(e["detail"]), "digest_frequency") {
		t.Errorf("detail should name changed settings: %v", e["detail"])
	}

	// The newsletter list's trail has its own settings.update, not dev's.
	newsEvents := auditEvents(t, baseURL, "/api/console/lists/example.com/news/audit", ownerCookies)
	if len(newsEvents) != 1 || newsEvents[0]["list_addr"] != "news@example.com" {
		t.Fatalf("news audit = %v, want one event scoped to news@example.com", newsEvents)
	}
}

func TestConsoleAuditModerationAndRoles(t *testing.T) {
	_, st, baseURL, disc, _ := consoleFixture(t)
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)

	// Approve a held message as the owner: moderation.approve is recorded.
	held := holdMessage(t, st, disc, "alice@example.com", "Hello audit")
	resp, _ := do(t, baseURL, "POST", "/api/console/lists/example.com/dev/held/"+fmt.Sprint(held.ID)+"/approve", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve held status = %d, want 200", resp.StatusCode)
	}

	// Grant the moderator role to alice.
	alice := mustSubscriber(t, st, "alice@example.com")
	resp, _ = do(t, baseURL, "POST", "/api/console/lists/example.com/dev/roles/"+fmt.Sprint(alice.ID)+"/moderator", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("grant moderator status = %d, want 200", resp.StatusCode)
	}

	events := auditEvents(t, baseURL, "/api/console/lists/example.com/dev/audit", ownerCookies)
	if len(events) != 2 {
		t.Fatalf("audit events = %d, want 2; got %v", len(events), events)
	}
	// Newest first: the role grant, then the moderation approve.
	if events[0]["action"] != "role.grant" || events[0]["detail"] != "moderator" {
		t.Errorf("events[0] = %v, want role.grant (moderator)", events[0])
	}
	if events[1]["action"] != "moderation.approve" || events[1]["target"] != "Hello audit" {
		t.Errorf("events[1] = %v, want moderation.approve on 'Hello audit'", events[1])
	}

	// Action filter returns only matching events.
	held2 := holdMessage(t, st, disc, "bob@example.com", "Second")
	resp, _ = do(t, baseURL, "POST", "/api/console/lists/example.com/dev/held/"+fmt.Sprint(held2.ID)+"/discard", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("discard held status = %d, want 200", resp.StatusCode)
	}
	modEvents := auditEvents(t, baseURL, "/api/console/lists/example.com/dev/audit?action=moderation.discard", ownerCookies)
	if len(modEvents) != 1 || modEvents[0]["action"] != "moderation.discard" {
		t.Fatalf("filtered audit = %v, want one moderation.discard", modEvents)
	}
}

func TestAdminAuditGateAndInstanceEvents(t *testing.T) {
	_, st, baseURL, _ := adminFixture(t)
	admin := makeSubscriber(t, st, "owner@example.com")
	adminCookies := login(t, st, baseURL, admin.Email)

	// Anonymous: 401. Non-admin subscriber: 403.
	resp, _ := do(t, baseURL, "GET", "/api/console/admin/audit", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous admin audit status = %d, want 401", resp.StatusCode)
	}
	alice := makeSubscriber(t, st, "alice@example.com")
	aliceCookies := login(t, st, baseURL, alice.Email)
	resp, _ = do(t, baseURL, "GET", "/api/console/admin/audit", "", aliceCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin audit status = %d, want 403", resp.StatusCode)
	}

	// Create a domain instance-wide: records an instance-level domain.create.
	resp, body := do(t, baseURL, "POST", "/api/console/admin/domains", `{"name":"newsite.org"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create domain status = %d; body=%s", resp.StatusCode, body)
	}

	// The instance-wide trail includes the domain.create plus the list events.
	events := auditEvents(t, baseURL, "/api/console/admin/audit", adminCookies)
	if len(events) == 0 {
		t.Fatal("admin audit is empty after domain.create")
	}
	if events[0]["action"] != "domain.create" {
		t.Errorf("events[0] = %v, want instance-level domain.create", events[0])
	}
	if _, ok := events[0]["list_addr"]; ok {
		t.Errorf("domain.create should carry no list_addr, got %v", events[0]["list_addr"])
	}
	if events[0]["actor_email"] != "owner@example.com" {
		t.Errorf("actor = %v, want owner@example.com", events[0]["actor_email"])
	}
}
