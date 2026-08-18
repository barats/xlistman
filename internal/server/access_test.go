package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestWebStatusDefault reports both switches enabled on a fresh store.
func TestWebStatusDefault(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	_ = st
	resp, body := do(t, baseURL, "GET", "/api/web-status", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("web-status status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, `"login_enabled":true`) || !strings.Contains(body, `"management_enabled":true`) {
		t.Errorf("web-status defaults = %s, want both enabled", body)
	}
}

// TestWebStatusReflectsToggles verifies the endpoint reports the store state.
func TestWebStatusReflectsToggles(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	ctx := context.Background()
	if err := st.SetWebLoginEnabled(ctx, false); err != nil {
		t.Fatalf("disable login: %v", err)
	}
	if err := st.SetWebManagementEnabled(ctx, false); err != nil {
		t.Fatalf("disable management: %v", err)
	}
	resp, body := do(t, baseURL, "GET", "/api/web-status", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("web-status status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, `"login_enabled":false`) || !strings.Contains(body, `"management_enabled":false`) {
		t.Errorf("web-status = %s, want both disabled", body)
	}
}

// TestLoginDisabledBlocksNewLogins verifies the magic-link flow is blocked and
// verify redirects to /auth?error=disabled (ADR 0020).
func TestLoginDisabledBlocksNewLogins(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	makeSubscriber(t, st, "alice@example.com")
	if err := st.SetWebLoginEnabled(context.Background(), false); err != nil {
		t.Fatalf("disable login: %v", err)
	}

	resp, body := do(t, baseURL, "POST", "/api/auth/magic-link", `{"email":"alice@example.com"}`, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("magic-link status = %d, want 403; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "login is disabled") {
		t.Errorf("magic-link body = %s, want disabled message", body)
	}

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(baseURL + "/api/auth/verify?token=whatever")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("verify status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "/auth?error=disabled") {
		t.Errorf("verify location = %q, want /auth?error=disabled", loc)
	}
}

// TestDisableLoginLogsEveryoneOut verifies an existing Session is ended when
// login is disabled (the CLI deletes all sessions).
func TestDisableLoginLogsEveryoneOut(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	ctx := context.Background()
	alice := makeSubscriber(t, st, "alice@example.com")
	cookies := login(t, st, baseURL, alice.Email)

	resp, _ := do(t, baseURL, "GET", "/api/me", "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me before disable status = %d, want 200", resp.StatusCode)
	}

	if err := st.SetWebLoginEnabled(ctx, false); err != nil {
		t.Fatalf("disable login: %v", err)
	}
	if _, err := st.DeleteAllSessions(ctx); err != nil {
		t.Fatalf("delete sessions: %v", err)
	}

	resp, _ = do(t, baseURL, "GET", "/api/me", "", cookies)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/api/me after disable status = %d, want 401", resp.StatusCode)
	}
}

// TestManagementDisabledBlocksConsoles verifies both consoles return 403 while
// subscriber self-service and public pages keep working, and that re-enabling
// restores access (ADR 0020).
func TestManagementDisabledBlocksConsoles(t *testing.T) {
	_, st, baseURL, _, _ := consoleFixture(t)
	ctx := context.Background()
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)

	resp, _ := do(t, baseURL, "GET", "/api/console/lists", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("console before disable status = %d, want 200", resp.StatusCode)
	}

	if err := st.SetWebManagementEnabled(ctx, false); err != nil {
		t.Fatalf("disable management: %v", err)
	}

	// Role console blocked.
	resp, body := do(t, baseURL, "GET", "/api/console/lists", "", ownerCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("console after disable status = %d, want 403; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "web management is disabled") {
		t.Errorf("console body = %s, want disabled message", body)
	}

	// Server admin area blocked, including the info endpoint.
	admin := makeSubscriber(t, st, "admin@example.com")
	if err := st.AddAdministrator(ctx, admin.ID); err != nil {
		t.Fatalf("add administrator: %v", err)
	}
	adminCookies := login(t, st, baseURL, admin.Email)
	resp, body = do(t, baseURL, "GET", "/api/console/admin/domains", "", adminCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin domains after disable status = %d, want 403; body=%s", resp.StatusCode, body)
	}
	resp, _ = do(t, baseURL, "GET", "/api/console/admin/info", "", adminCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin info after disable status = %d, want 403", resp.StatusCode)
	}

	// Self-service still works.
	resp, _ = do(t, baseURL, "GET", "/api/me", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me after disable status = %d, want 200", resp.StatusCode)
	}

	// Public pages still work.
	resp, _ = do(t, baseURL, "GET", "/api/lists", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/lists after disable status = %d, want 200", resp.StatusCode)
	}

	// Re-enable restores the console.
	if err := st.SetWebManagementEnabled(ctx, true); err != nil {
		t.Fatalf("re-enable management: %v", err)
	}
	resp, _ = do(t, baseURL, "GET", "/api/console/lists", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("console after re-enable status = %d, want 200", resp.StatusCode)
	}
}

// TestManagementDisabledStillAllowsLogin verifies a user can still sign in
// when only management is disabled (sessions are only wiped by login disable).
func TestManagementDisabledStillAllowsLogin(t *testing.T) {
	_, st, baseURL, _, _ := consoleFixture(t)
	if err := st.SetWebManagementEnabled(context.Background(), false); err != nil {
		t.Fatalf("disable management: %v", err)
	}
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)
	resp, _ := do(t, baseURL, "GET", "/api/me", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me with fresh login status = %d, want 200", resp.StatusCode)
	}
}
