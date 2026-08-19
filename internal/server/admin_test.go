package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

// adminFixture returns a server with the owner@example.com subscriber
// designated as an Administrator and a discussion list.
func adminFixture(t *testing.T) (*Server, *sqlite.Store, string, *model.List) {
	t.Helper()
	srv, st, baseURL := newTestServer(t)
	ctx := context.Background()
	disc := setupList(t, st)
	admin := makeSubscriber(t, st, "owner@example.com")
	if err := st.AddAdministrator(ctx, admin.ID); err != nil {
		t.Fatalf("add administrator: %v", err)
	}
	return srv, st, baseURL, disc
}

func TestAdminInfo(t *testing.T) {
	_, st, baseURL, _ := adminFixture(t)

	// Anonymous: 401.
	resp, _ := do(t, baseURL, "GET", "/api/console/admin/info", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous info status = %d, want 401", resp.StatusCode)
	}

	// Non-admin subscriber: is_administrator false.
	alice := makeSubscriber(t, st, "alice@example.com")
	aliceCookies := login(t, st, baseURL, alice.Email)
	resp, body := do(t, baseURL, "GET", "/api/console/admin/info", "", aliceCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscriber info status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, `"is_administrator":false`) {
		t.Errorf("non-admin info = %s, want is_administrator false", body)
	}

	// Administrator: is_administrator true.
	admin := makeSubscriber(t, st, "owner@example.com")
	adminCookies := login(t, st, baseURL, admin.Email)
	resp, body = do(t, baseURL, "GET", "/api/console/admin/info", "", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin info status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, `"is_administrator":true`) {
		t.Errorf("admin info = %s, want is_administrator true", body)
	}

	// /api/me reports the flag too.
	resp, body = do(t, baseURL, "GET", "/api/me", "", adminCookies)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"is_administrator":true`) {
		t.Errorf("/api/me admin flag missing: status=%d body=%s", resp.StatusCode, body)
	}
}

func TestAdminGate(t *testing.T) {
	_, st, baseURL, _ := adminFixture(t)
	alice := makeSubscriber(t, st, "alice@example.com")
	aliceCookies := login(t, st, baseURL, alice.Email)

	for _, path := range []string{
		"/api/console/admin/domains",
		"/api/console/admin/lists",
		"/api/console/admin/administrators",
	} {
		resp, _ := do(t, baseURL, "GET", path, "", aliceCookies)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("non-admin GET %s status = %d, want 403", path, resp.StatusCode)
		}
	}
	// Anonymous: 401.
	resp, _ := do(t, baseURL, "GET", "/api/console/admin/domains", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous admin status = %d, want 401", resp.StatusCode)
	}
}

func TestAdminDomains(t *testing.T) {
	_, st, baseURL, _ := adminFixture(t)
	admin := makeSubscriber(t, st, "owner@example.com")
	adminCookies := login(t, st, baseURL, admin.Email)

	// Create a domain.
	resp, body := do(t, baseURL, "POST", "/api/console/admin/domains", `{"name":"lists.example.org","description":"Lists"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create domain status = %d, want 201; body=%s", resp.StatusCode, body)
	}

	// It lists.
	resp, body = do(t, baseURL, "GET", "/api/console/admin/domains", "", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list domains status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "lists.example.org") {
		t.Errorf("domains missing new domain: %s", body)
	}
	if !strings.Contains(body, "example.com") {
		t.Errorf("domains missing existing domain: %s", body)
	}

	// Duplicate is 409.
	resp, _ = do(t, baseURL, "POST", "/api/console/admin/domains", `{"name":"lists.example.org"}`, adminCookies)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate domain status = %d, want 409", resp.StatusCode)
	}

	// Invalid names are 400.
	for _, bad := range []string{"not a domain", "nodots", "has@at.example"} {
		resp, _ = do(t, baseURL, "POST", "/api/console/admin/domains", `{"name":"`+bad+`"}`, adminCookies)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("invalid domain %q status = %d, want 400", bad, resp.StatusCode)
		}
	}
}

func TestAdminCreateList(t *testing.T) {
	_, st, baseURL, _ := adminFixture(t)
	ctx := context.Background()
	admin := makeSubscriber(t, st, "owner@example.com")
	adminCookies := login(t, st, baseURL, admin.Email)

	// Create a list; the creating Administrator becomes the first Owner.
	resp, body := do(t, baseURL, "POST", "/api/console/admin/lists",
		`{"list_name":"announce","domain":"example.com","list_type":"newsletter","description":"Announcements"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create list status = %d, want 201; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "announce@example.com") {
		t.Errorf("create list response missing address: %s", body)
	}
	announce, err := st.GetList(ctx, "announce", "example.com")
	if err != nil {
		t.Fatalf("list not created: %v", err)
	}
	if announce.ListType != model.ListTypeNewsletter {
		t.Errorf("list type = %q, want newsletter", announce.ListType)
	}
	if ok, _ := st.IsOwner(ctx, announce.ID, admin.ID); !ok {
		t.Errorf("creator not the first owner")
	}

	// Create a discussion list with a designated first owner and moderation on.
	bob := makeSubscriber(t, st, "bob@example.com")
	resp, body = do(t, baseURL, "POST", "/api/console/admin/lists",
		`{"list_name":"team","domain":"example.com","list_type":"discussion","first_owner_email":"bob@example.com","moderation":true}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create list 2 status = %d, want 201; body=%s", resp.StatusCode, body)
	}
	team, _ := st.GetList(ctx, "team", "example.com")
	if ok, _ := st.IsOwner(ctx, team.ID, bob.ID); !ok {
		t.Errorf("specified first owner not assigned")
	}
	if !team.Settings.ModerationEnabled {
		t.Errorf("moderation not enabled for discussion list")
	}

	// Duplicate address is 409.
	resp, _ = do(t, baseURL, "POST", "/api/console/admin/lists", `{"list_name":"team","domain":"example.com","list_type":"discussion"}`, adminCookies)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate list status = %d, want 409", resp.StatusCode)
	}

	// Unknown domain is 400.
	resp, body = do(t, baseURL, "POST", "/api/console/admin/lists", `{"list_name":"x","domain":"nope.com","list_type":"discussion"}`, adminCookies)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "domain not found") {
		t.Fatalf("unknown domain status = %d, want 400 with guidance; body=%s", resp.StatusCode, body)
	}

	// Invalid list name is 400.
	resp, _ = do(t, baseURL, "POST", "/api/console/admin/lists", `{"list_name":"bad name","domain":"example.com","list_type":"discussion"}`, adminCookies)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid list name status = %d, want 400", resp.StatusCode)
	}

	// Invalid type is 400.
	resp, _ = do(t, baseURL, "POST", "/api/console/admin/lists", `{"list_name":"x2","domain":"example.com","list_type":"digest"}`, adminCookies)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid type status = %d, want 400", resp.StatusCode)
	}

	// The admin lists endpoint shows all lists with member counts.
	resp, body = do(t, baseURL, "GET", "/api/console/admin/lists", "", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin list lists status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "announce@example.com") || !strings.Contains(body, "team@example.com") {
		t.Errorf("admin lists missing lists: %s", body)
	}
}

func TestAdminDeleteList(t *testing.T) {
	_, st, baseURL, disc := adminFixture(t)
	ctx := context.Background()
	admin := makeSubscriber(t, st, "owner@example.com")
	adminCookies := login(t, st, baseURL, admin.Email)

	// Seed related data so we can verify the cascade through the API.
	addMember(t, st, disc, "alice@example.com")
	st.ArchiveMessage(ctx, disc.ID, "<m1@x>", "gone soon", "alice@example.com", []byte("body"), "t1", "body")

	resp, body := do(t, baseURL, "DELETE", "/api/console/admin/lists/example.com/dev", "", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete list status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	if _, err := st.GetList(ctx, "dev", "example.com"); err == nil {
		t.Errorf("list still exists after web delete")
	}
	if entries, _ := st.ListArchive(ctx, disc.ID, 10, 0); len(entries) != 0 {
		t.Errorf("archive remains after web delete: %d", len(entries))
	}
	if subs, _ := st.ListSubscriptions(ctx, disc.ID); len(subs) != 0 {
		t.Errorf("subscriptions remain after web delete: %d", len(subs))
	}

	// Deleting an unknown list is 404.
	resp, _ = do(t, baseURL, "DELETE", "/api/console/admin/lists/example.com/ghost", "", adminCookies)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("delete unknown list status = %d, want 404", resp.StatusCode)
	}
}

func TestAdminListType(t *testing.T) {
	_, st, baseURL, disc := adminFixture(t)
	ctx := context.Background()
	admin := makeSubscriber(t, st, "owner@example.com")
	adminCookies := login(t, st, baseURL, admin.Email)

	resp, body := do(t, baseURL, "POST", "/api/console/admin/lists/example.com/dev/type", `{"list_type":"newsletter"}`, adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change type status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	got, _ := st.GetListByID(ctx, disc.ID)
	if got.ListType != model.ListTypeNewsletter {
		t.Errorf("list type = %q, want newsletter", got.ListType)
	}

	resp, _ = do(t, baseURL, "POST", "/api/console/admin/lists/example.com/dev/type", `{"list_type":"digest"}`, adminCookies)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid type status = %d, want 400", resp.StatusCode)
	}
}

func TestAdminAdministrators(t *testing.T) {
	_, st, baseURL, _ := adminFixture(t)
	ctx := context.Background()
	admin := makeSubscriber(t, st, "owner@example.com")
	adminCookies := login(t, st, baseURL, admin.Email)

	// List shows the designated admin.
	resp, body := do(t, baseURL, "GET", "/api/console/admin/administrators", "", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list administrators status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "owner@example.com") {
		t.Errorf("administrators missing owner: %s", body)
	}

	// Designate a known subscriber.
	carol := makeSubscriber(t, st, "carol@example.com")
	resp, body = do(t, baseURL, "POST", "/api/console/admin/administrators", `{"email":"carol@example.com"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add administrator status = %d, want 201; body=%s", resp.StatusCode, body)
	}
	if ok, _ := st.IsAdministrator(ctx, carol.ID); !ok {
		t.Errorf("carol not an administrator after add")
	}

	// Subscriber-first: unknown emails are rejected with guidance.
	resp, body = do(t, baseURL, "POST", "/api/console/admin/administrators", `{"email":"nobody@example.com"}`, adminCookies)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "unknown subscriber") {
		t.Fatalf("add unknown admin status = %d, want 404 with guidance; body=%s", resp.StatusCode, body)
	}

	// Revoke carol.
	resp, _ = do(t, baseURL, "DELETE", "/api/console/admin/administrators/"+itoa(carol.ID), "", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove administrator status = %d, want 200", resp.StatusCode)
	}
	if ok, _ := st.IsAdministrator(ctx, carol.ID); ok {
		t.Errorf("carol still an administrator after remove")
	}

	// Revoked carol can no longer use admin endpoints.
	carolCookies := login(t, st, baseURL, carol.Email)
	resp, _ = do(t, baseURL, "GET", "/api/console/admin/domains", "", carolCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("revoked admin status = %d, want 403", resp.StatusCode)
	}
}

// TestAdminMeFlag verifies /api/me carries is_administrator so the UI can
// show the server-admin link.
func TestAdminMeFlag(t *testing.T) {
	_, st, baseURL, _ := adminFixture(t)
	admin := makeSubscriber(t, st, "owner@example.com")
	adminCookies := login(t, st, baseURL, admin.Email)

	resp, body := do(t, baseURL, "GET", "/api/me", "", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me status = %d, want 200", resp.StatusCode)
	}
	var me struct {
		IsAdministrator bool `json:"is_administrator"`
	}
	if err := json.Unmarshal([]byte(body), &me); err != nil {
		t.Fatalf("unmarshal /api/me: %v", err)
	}
	if !me.IsAdministrator {
		t.Errorf("is_administrator = false, want true")
	}
}
