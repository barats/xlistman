package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

// consoleFixture sets up an in-memory server with a moderated discussion list
// (owned by owner, moderated by mod, subscribed by alice) and a newsletter
// list owned by the same owner.
func consoleFixture(t *testing.T) (*Server, *sqlite.Store, string, *model.List, *model.List) {
	t.Helper()
	srv, st, baseURL := newTestServer(t)
	ctx := context.Background()

	disc := setupList(t, st)
	settings := disc.Settings
	settings.ModerationEnabled = true
	if err := st.UpdateListSettings(ctx, disc.ID, settings); err != nil {
		t.Fatalf("enable moderation: %v", err)
	}

	makeSubscriber(t, st, "owner@example.com")
	if err := st.AddOwner(ctx, disc.ID, mustSubscriber(t, st, "owner@example.com").ID); err != nil {
		t.Fatalf("add owner: %v", err)
	}
	mod := makeSubscriber(t, st, "mod@example.com")
	if err := st.AddModerator(ctx, disc.ID, mod.ID); err != nil {
		t.Fatalf("add moderator: %v", err)
	}
	addMember(t, st, disc, "alice@example.com")

	d, err := st.GetDomain(ctx, "example.com")
	if err != nil {
		t.Fatalf("get domain: %v", err)
	}
	news, err := st.CreateList(ctx, "news", d.ID, "example.com", "News", model.ListTypeNewsletter)
	if err != nil {
		t.Fatalf("create newsletter: %v", err)
	}
	if err := st.AddOwner(ctx, news.ID, mustSubscriber(t, st, "owner@example.com").ID); err != nil {
		t.Fatalf("add owner to newsletter: %v", err)
	}

	return srv, st, baseURL, disc, news
}

func makeSubscriber(t *testing.T, st *sqlite.Store, email string) *model.Subscriber {
	t.Helper()
	sub, err := st.GetOrCreateSubscriber(context.Background(), email)
	if err != nil {
		t.Fatalf("get or create subscriber: %v", err)
	}
	return sub
}

func holdMessage(t *testing.T, st *sqlite.Store, l *model.List, sender, subject string) *model.HeldMessage {
	t.Helper()
	body := []byte("From: " + sender + "\r\nSubject: " + subject + "\r\n\r\nbody\r\n")
	m, err := st.CreateHeldMessage(context.Background(), l.ID, sender, subject, body, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create held message: %v", err)
	}
	return m
}

// queuedTo returns the envelope recipients of every queued message.
func queuedTo(t *testing.T, st *sqlite.Store, ctx context.Context) []string {
	t.Helper()
	queued, err := st.ListQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var to []string
	for _, q := range queued {
		to = append(to, q.To)
	}
	return to
}

func TestConsoleListsShowsRoles(t *testing.T) {
	_, st, baseURL, disc, _ := consoleFixture(t)

	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)
	resp, body := do(t, baseURL, "GET", "/api/console/lists", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner console status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("owner console lists = %d, want 2 (disc + news)", len(got))
	}
	for _, l := range got {
		if l["address"] == disc.Address() {
			roles, _ := json.Marshal(l["roles"])
			if !strings.Contains(string(roles), "owner") {
				t.Errorf("disc roles = %s, want owner", roles)
			}
		}
	}

	// A moderator sees only the discussion list.
	mod := makeSubscriber(t, st, "mod@example.com")
	modCookies := login(t, st, baseURL, mod.Email)
	resp, body = do(t, baseURL, "GET", "/api/console/lists", "", modCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mod console status = %d, want 200", resp.StatusCode)
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("mod console lists = %d, want 1 (disc only)", len(got))
	}

	// A plain subscriber sees nothing.
	alice := makeSubscriber(t, st, "alice@example.com")
	aliceCookies := login(t, st, baseURL, alice.Email)
	resp, body = do(t, baseURL, "GET", "/api/console/lists", "", aliceCookies)
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("plain subscriber console lists = %d, want 0", len(got))
	}
}

func TestConsoleHeldRoleGate(t *testing.T) {
	_, st, baseURL, disc, _ := consoleFixture(t)
	holdMessage(t, st, disc, "charlie@example.com", "please approve")

	// Unsigned: 401.
	resp, _ := do(t, baseURL, "GET", "/api/console/lists/example.com/dev/held", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned held status = %d, want 401", resp.StatusCode)
	}

	// Signed-in non-role subscriber: 403.
	alice := makeSubscriber(t, st, "alice@example.com")
	aliceCookies := login(t, st, baseURL, alice.Email)
	resp, _ = do(t, baseURL, "GET", "/api/console/lists/example.com/dev/held", "", aliceCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-role held status = %d, want 403", resp.StatusCode)
	}

	// Moderator: 200 with the held message.
	mod := makeSubscriber(t, st, "mod@example.com")
	modCookies := login(t, st, baseURL, mod.Email)
	resp, body := do(t, baseURL, "GET", "/api/console/lists/example.com/dev/held", "", modCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("moderator held status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("held messages = %d, want 1", len(got))
	}
	if got[0]["subject"] != "please approve" {
		t.Errorf("held subject = %v, want 'please approve'", got[0]["subject"])
	}

	// Detail returns the body for a moderator.
	resp, body = do(t, baseURL, "GET", "/api/console/lists/example.com/dev/held/"+strconv.FormatInt(int64(got[0]["id"].(float64)), 10), "", modCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("held detail status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "body") {
		t.Errorf("held detail missing body: %s", body)
	}
}

func TestConsoleHeldActions(t *testing.T) {
	_, st, baseURL, disc, _ := consoleFixture(t)
	ctx := context.Background()
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)
	base := "/api/console/lists/example.com/dev/held"

	// Approve: held message removed and delivered to the active subscriber.
	holdMessage(t, st, disc, "charlie@example.com", "approve me")
	resp, body := do(t, baseURL, "POST", base+"/1/approve", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	held, _ := st.ListHeldMessages(ctx, disc.ID)
	if len(held) != 0 {
		t.Errorf("held messages remain after approve")
	}
	if to := strings.Join(queuedTo(t, st, ctx), ","); !strings.Contains(to, "alice@example.com") {
		t.Errorf("approved post not delivered to alice; got %q", to)
	}

	// Reject: held message removed and a rejection notice is sent.
	holdMessage(t, st, disc, "dave@example.com", "reject me")
	resp, body = do(t, baseURL, "POST", base+"/2/reject", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reject status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	held, _ = st.ListHeldMessages(ctx, disc.ID)
	if len(held) != 0 {
		t.Errorf("held messages remain after reject")
	}
	queued, _ := st.ListQueued(ctx)
	found := false
	for _, q := range queued {
		if q.To == "dave@example.com" && strings.Contains(string(q.Body), "reject") {
			found = true
		}
	}
	if !found {
		t.Errorf("rejection notice not sent to dave")
	}

	// Discard: held message removed silently (no new mail to the sender).
	holdMessage(t, st, disc, "erin@example.com", "discard me")
	before := len(queuedTo(t, st, ctx))
	resp, body = do(t, baseURL, "POST", base+"/3/discard", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("discard status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	held, _ = st.ListHeldMessages(ctx, disc.ID)
	if len(held) != 0 {
		t.Errorf("held messages remain after discard")
	}
	if after := len(queuedTo(t, st, ctx)); after != before {
		t.Errorf("discard enqueued messages: before=%d after=%d, want no change", before, after)
	}
}

func TestConsoleHeldActionRoleGate(t *testing.T) {
	_, st, baseURL, disc, _ := consoleFixture(t)
	holdMessage(t, st, disc, "charlie@example.com", "nope")

	alice := makeSubscriber(t, st, "alice@example.com")
	aliceCookies := login(t, st, baseURL, alice.Email)
	resp, _ := do(t, baseURL, "POST", "/api/console/lists/example.com/dev/held/1/approve", "", aliceCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-role action status = %d, want 403", resp.StatusCode)
	}
	held, _ := st.ListHeldMessages(context.Background(), disc.ID)
	if len(held) != 1 {
		t.Errorf("held message removed by unauthorized action")
	}
}

func TestConsoleSenders(t *testing.T) {
	_, st, baseURL, _, news := consoleFixture(t)
	ctx := context.Background()
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)

	// Add a known subscriber as a designated sender.
	carol := makeSubscriber(t, st, "carol@example.com")
	resp, body := do(t, baseURL, "POST", "/api/console/lists/example.com/news/senders", `{"email":"carol@example.com"}`, ownerCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add sender status = %d, want 201; body=%s", resp.StatusCode, body)
	}

	// It appears in the list.
	resp, body = do(t, baseURL, "GET", "/api/console/lists/example.com/news/senders", "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list senders status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "carol@example.com") {
		t.Errorf("sender list missing carol: %s", body)
	}

	// Adding an unknown email is Subscriber-first: 404 with guidance.
	resp, body = do(t, baseURL, "POST", "/api/console/lists/example.com/news/senders", `{"email":"nobody@example.com"}`, ownerCookies)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("add unknown sender status = %d, want 404; body=%s", resp.StatusCode, body)
	}

	// A moderator (not owner) cannot manage the allowlist.
	mod := makeSubscriber(t, st, "mod@example.com")
	if err := st.AddModerator(ctx, news.ID, mod.ID); err != nil {
		t.Fatalf("add moderator: %v", err)
	}
	modCookies := login(t, st, baseURL, mod.Email)
	resp, _ = do(t, baseURL, "POST", "/api/console/lists/example.com/news/senders", `{"email":"carol@example.com"}`, modCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("moderator add sender status = %d, want 403", resp.StatusCode)
	}

	// A discussion list has no allowlist.
	resp, _ = do(t, baseURL, "POST", "/api/console/lists/example.com/dev/senders", `{"email":"carol@example.com"}`, ownerCookies)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("discussion add sender status = %d, want 400", resp.StatusCode)
	}

	// Remove by subscriber id.
	resp, body = do(t, baseURL, "DELETE", "/api/console/lists/example.com/news/senders/"+fmt.Sprint(carol.ID), "", ownerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove sender status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	resp, body = do(t, baseURL, "GET", "/api/console/lists/example.com/news/senders", "", ownerCookies)
	if strings.Contains(body, "carol@example.com") {
		t.Errorf("sender still listed after remove: %s", body)
	}
}
