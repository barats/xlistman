package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TestMyHeldPosts verifies the sender held-status view: an authenticated
// subscriber sees only their own posts currently awaiting approval, matched
// case-insensitively, with the list address and expiry.
func TestMyHeldPosts(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	ctx := context.Background()
	disc := setupList(t, st)
	settings := disc.Settings
	settings.ModerationEnabled = true
	if err := st.UpdateListSettings(ctx, disc.ID, settings); err != nil {
		t.Fatal(err)
	}

	charlie := makeSubscriber(t, st, "charlie@example.com")
	addMember(t, st, disc, "charlie@example.com")
	bob := makeSubscriber(t, st, "bob@example.com")
	addMember(t, st, disc, "bob@example.com")

	// Both subscribers have a held post; bob's stored with mixed case to
	// exercise the case-insensitive sender match.
	if _, err := st.CreateHeldMessage(ctx, disc.ID, "charlie@example.com", "charlie post", []byte("x"), time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateHeldMessage(ctx, disc.ID, "Bob@Example.com", "bob post", []byte("x"), time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Anonymous: 401.
	resp, _ := do(t, baseURL, "GET", "/api/me/held-posts", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", resp.StatusCode)
	}

	// charlie sees only their own held post, with list address and expiry.
	charlieCookies := login(t, st, baseURL, charlie.Email)
	resp, body := do(t, baseURL, "GET", "/api/me/held-posts", "", charlieCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("charlie status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	var posts []map[string]any
	if err := json.Unmarshal([]byte(body), &posts); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 {
		t.Fatalf("charlie held posts = %d, want 1; %s", len(posts), body)
	}
	if posts[0]["subject"] != "charlie post" || posts[0]["list_addr"] != "dev@example.com" {
		t.Errorf("post = %v, want charlie post on dev@example.com", posts[0])
	}
	if posts[0]["expires_at"] == "" {
		t.Errorf("post missing expires_at: %v", posts[0])
	}

	// bob sees their own held post (case-insensitive match on the sender).
	bobCookies := login(t, st, baseURL, bob.Email)
	resp, body = do(t, baseURL, "GET", "/api/me/held-posts", "", bobCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bob status = %d, want 200", resp.StatusCode)
	}
	if err := json.Unmarshal([]byte(body), &posts); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0]["subject"] != "bob post" {
		t.Fatalf("bob held posts = %v, want one 'bob post'", posts)
	}

	// A subscriber with no held posts gets an empty list, not an error.
	dave := makeSubscriber(t, st, "dave@example.com")
	daveCookies := login(t, st, baseURL, dave.Email)
	resp, body = do(t, baseURL, "GET", "/api/me/held-posts", "", daveCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dave status = %d, want 200", resp.StatusCode)
	}
	if err := json.Unmarshal([]byte(body), &posts); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 0 {
		t.Fatalf("dave held posts = %v, want none", posts)
	}
}
