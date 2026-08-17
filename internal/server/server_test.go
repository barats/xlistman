package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/barats/xlistman/internal/config"
	xmail "github.com/barats/xlistman/internal/mail"
	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

func newTestServer(t *testing.T) (*Server, *sqlite.Store, string) {
	t.Helper()
	st, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory store: %v", err)
	}
	cfg := &config.Config{Web: config.WebConfig{BaseURL: "http://test.local"}}
	pipeline := &xmail.Pipeline{Store: st, WebBaseURL: cfg.Web.BaseURL}
	srv := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), pipeline)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, st, ts.URL
}

func setupList(t *testing.T, st *sqlite.Store) *model.List {
	t.Helper()
	ctx := context.Background()
	if _, err := st.CreateDomain(ctx, "example.com", "Example"); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	d, err := st.GetDomain(ctx, "example.com")
	if err != nil {
		t.Fatalf("get domain: %v", err)
	}
	l, err := st.CreateList(ctx, "dev", d.ID, "example.com", "Dev list", model.ListTypeDiscussion)
	if err != nil {
		t.Fatalf("create list: %v", err)
	}
	return l
}

func addMember(t *testing.T, st *sqlite.Store, l *model.List, email string) (*model.Subscriber, *model.Subscription) {
	t.Helper()
	ctx := context.Background()
	sub, err := st.GetOrCreateSubscriber(ctx, email)
	if err != nil {
		t.Fatalf("get or create subscriber: %v", err)
	}
	s, err := st.CreateSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if err := st.ConfirmSubscription(ctx, s.ID, model.SubscriptionStatusActive); err != nil {
		t.Fatalf("confirm subscription: %v", err)
	}
	return sub, s
}

func do(t *testing.T, baseURL, method, path, body string, cookies []*http.Cookie) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, baseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, string(b)
}

func queuedCount(t *testing.T, st *sqlite.Store) int {
	t.Helper()
	qs, err := st.ListQueued(context.Background())
	if err != nil {
		t.Fatalf("list queued: %v", err)
	}
	return len(qs)
}

func queuedBodies(t *testing.T, st *sqlite.Store) []string {
	t.Helper()
	qs, err := st.ListQueued(context.Background())
	if err != nil {
		t.Fatalf("list queued: %v", err)
	}
	var out []string
	for _, q := range qs {
		out = append(out, string(q.Body))
	}
	return out
}

func login(t *testing.T, st *sqlite.Store, baseURL, email string) []*http.Cookie {
	t.Helper()
	resp, _ := do(t, baseURL, "POST", "/api/auth/magic-link", `{"email":"`+email+`"}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("magic-link status = %d, want 202", resp.StatusCode)
	}
	var token string
	for _, body := range queuedBodies(t, st) {
		// Only consider mail addressed to this subscriber.
		if !strings.Contains(body, "To: "+email+"\r\n") {
			continue
		}
		// The emailed link must target the API verify endpoint, not an SPA
		// route (the SPA has no /auth/verify page).
		if i := strings.Index(body, "/api/auth/verify?token="); i >= 0 {
			rest := body[i+len("/api/auth/verify?token="):]
			if j := strings.IndexAny(rest, "\r\n "); j >= 0 {
				rest = rest[:j]
			}
			token = rest
			break
		}
	}
	if token == "" {
		t.Fatalf("no magic link token found in queued mail")
	}
	// The email enqueued by login is the last one; verify consumes it.
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := http.NewRequest("GET", baseURL+"/api/auth/verify?token="+token, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("verify status = %d, want 302", resp.StatusCode)
	}
	return resp.Cookies()
}

func TestWebSubscribeFlow(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)

	resp, _ := do(t, baseURL, "POST", "/api/lists/example.com/dev/subscribe", `{"email":"alice@example.com"}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("subscribe status = %d, want 202", resp.StatusCode)
	}
	if got := queuedCount(t, st); got != 1 {
		t.Fatalf("queued after subscribe = %d, want 1 (confirmation email)", got)
	}

	sub, err := st.GetSubscription(context.Background(), l.ID, mustSubscriber(t, st, "alice@example.com").ID)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	if sub.Status != model.SubscriptionStatusPending {
		t.Fatalf("status = %s, want pending", sub.Status)
	}

	// A repeat request re-sends the confirmation instead of erroring.
	resp, _ = do(t, baseURL, "POST", "/api/lists/example.com/dev/subscribe", `{"email":"alice@example.com"}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("repeat subscribe status = %d, want 202", resp.StatusCode)
	}
	if got := queuedCount(t, st); got != 2 {
		t.Fatalf("queued after repeat subscribe = %d, want 2", got)
	}
}

func TestWebSubscribeClosedList(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	settings := l.Settings
	settings.SubscriptionPolicy = model.SubscriptionPolicyClosed
	if err := st.UpdateListSettings(context.Background(), l.ID, settings); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	resp, _ := do(t, baseURL, "POST", "/api/lists/example.com/dev/subscribe", `{"email":"alice@example.com"}`, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("subscribe closed status = %d, want 403", resp.StatusCode)
	}
}

func TestWebSubscribeAlreadyActive(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	addMember(t, st, l, "alice@example.com")

	resp, _ := do(t, baseURL, "POST", "/api/lists/example.com/dev/subscribe", `{"email":"alice@example.com"}`, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("subscribe existing status = %d, want 409", resp.StatusCode)
	}
}

func TestWebSubscribeBadEmail(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	setupList(t, st)

	resp, _ := do(t, baseURL, "POST", "/api/lists/example.com/dev/subscribe", `{"email":"not-an-email"}`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("subscribe bad email status = %d, want 400", resp.StatusCode)
	}
}

func TestMagicLinkLogin(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	addMember(t, st, l, "alice@example.com")

	// Unknown email gets 202 but no mail (no enumeration).
	before := queuedCount(t, st)
	resp, _ := do(t, baseURL, "POST", "/api/auth/magic-link", `{"email":"nobody@example.com"}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("magic-link unknown status = %d, want 202", resp.StatusCode)
	}
	if got := queuedCount(t, st); got != before {
		t.Fatalf("magic-link unknown queued = %d, want %d (no mail sent)", got, before)
	}

	cookies := login(t, st, baseURL, "alice@example.com")

	// Session cookie present; /api/me works.
	var cookie *http.Cookie
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("no session cookie in login response")
	}

	resp, body := do(t, baseURL, "GET", "/api/me", "", []*http.Cookie{cookie})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me status = %d, want 200", resp.StatusCode)
	}
	var me struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal([]byte(body), &me); err != nil {
		t.Fatalf("unmarshal /api/me: %v", err)
	}
	if me.Email != "alice@example.com" {
		t.Fatalf("/api/me email = %q, want alice@example.com", me.Email)
	}
}

func TestMagicLinkConsumedOnce(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	addMember(t, st, l, "alice@example.com")

	// Grab the token from the queued mail.
	do(t, baseURL, "POST", "/api/auth/magic-link", `{"email":"alice@example.com"}`, nil)
	var token string
	for _, body := range queuedBodies(t, st) {
		if i := strings.Index(body, "token="); i >= 0 {
			rest := body[i+len("token="):]
			if j := strings.IndexAny(rest, "\r\n "); j >= 0 {
				rest = rest[:j]
			}
			token = rest
		}
	}
	if token == "" {
		t.Fatalf("no token found")
	}

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	get := func() int {
		req, _ := http.NewRequest("GET", baseURL+"/api/auth/verify?token="+token, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}

	if got := get(); got != http.StatusFound {
		t.Fatalf("first verify status = %d, want 302", got)
	}
	if got := get(); got != http.StatusFound {
		t.Fatalf("second verify status = %d, want redirect to error (token consumed)", got)
	}
	// Consumed token should redirect to the auth error path, not a valid session.
	req, _ := http.NewRequest("GET", baseURL+"/api/auth/verify?token="+token, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	defer resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "error=invalid") {
		t.Fatalf("consumed token Location = %q, want error=invalid", loc)
	}
}

func TestMeRequiresAuth(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	setupList(t, st)

	resp, _ := do(t, baseURL, "GET", "/api/me", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/api/me status = %d, want 401", resp.StatusCode)
	}
}

func TestMeHasListRole(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	ctx := context.Background()

	// A subscriber with no list role reports has_list_role false.
	nobody := makeSubscriber(t, st, "nobody@example.com")
	nobodyCookies := login(t, st, baseURL, nobody.Email)
	resp, body := do(t, baseURL, "GET", "/api/me", "", nobodyCookies)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"has_list_role":false`) {
		t.Fatalf("/api/me no-role: status=%d body=%s", resp.StatusCode, body)
	}

	// An owner reports has_list_role true.
	owner := makeSubscriber(t, st, "owner@example.com")
	if err := st.AddOwner(ctx, l.ID, owner.ID); err != nil {
		t.Fatalf("add owner: %v", err)
	}
	ownerCookies := login(t, st, baseURL, owner.Email)
	resp, body = do(t, baseURL, "GET", "/api/me", "", ownerCookies)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"has_list_role":true`) {
		t.Fatalf("/api/me owner: status=%d body=%s", resp.StatusCode, body)
	}

	// An administrator with no list role: is_administrator true, has_list_role false.
	admin := makeSubscriber(t, st, "admin@example.com")
	if err := st.AddAdministrator(ctx, admin.ID); err != nil {
		t.Fatalf("add administrator: %v", err)
	}
	adminCookies := login(t, st, baseURL, admin.Email)
	resp, body = do(t, baseURL, "GET", "/api/me", "", adminCookies)
	if resp.StatusCode != http.StatusOK ||
		!strings.Contains(body, `"is_administrator":true`) ||
		!strings.Contains(body, `"has_list_role":false`) {
		t.Fatalf("/api/me admin: status=%d body=%s", resp.StatusCode, body)
	}
}

func TestSelfService(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	_, aliceSub := addMember(t, st, l, "alice@example.com")
	cookies := login(t, st, baseURL, "alice@example.com")

	resp, body := do(t, baseURL, "GET", "/api/me", "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me status = %d", resp.StatusCode)
	}
	var me struct {
		Subscriptions []struct {
			ID           int64  `json:"id"`
			Status       string `json:"status"`
			DeliveryMode string `json:"delivery_mode"`
			Address      string `json:"address"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal([]byte(body), &me); err != nil {
		t.Fatalf("unmarshal /api/me: %v", err)
	}
	if len(me.Subscriptions) != 1 || me.Subscriptions[0].ID != aliceSub.ID {
		t.Fatalf("subscriptions = %+v, want one entry for the dev list", me.Subscriptions)
	}

	// Set delivery to digest.
	resp, _ = do(t, baseURL, "POST", "/api/me/subscriptions/"+itoa(aliceSub.ID)+"/delivery", `{"mode":"digest"}`, cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set delivery status = %d, want 200", resp.StatusCode)
	}
	s, _ := st.GetSubscriptionByID(context.Background(), aliceSub.ID)
	if s.DeliveryMode != model.DeliveryModeDigest {
		t.Fatalf("delivery mode = %s, want digest", s.DeliveryMode)
	}

	// Invalid mode rejected.
	resp, _ = do(t, baseURL, "POST", "/api/me/subscriptions/"+itoa(aliceSub.ID)+"/delivery", `{"mode":"daily"}`, cookies)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid delivery status = %d, want 400", resp.StatusCode)
	}

	// Re-enable a disabled subscription.
	if err := st.SetSubscriptionStatus(context.Background(), aliceSub.ID, model.SubscriptionStatusDisabled); err != nil {
		t.Fatalf("disable: %v", err)
	}
	resp, _ = do(t, baseURL, "POST", "/api/me/subscriptions/"+itoa(aliceSub.ID)+"/re-enable", "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-enable status = %d, want 200", resp.StatusCode)
	}
	s, _ = st.GetSubscriptionByID(context.Background(), aliceSub.ID)
	if s.Status != model.SubscriptionStatusActive {
		t.Fatalf("status after re-enable = %s, want active", s.Status)
	}
	resp, _ = do(t, baseURL, "POST", "/api/me/subscriptions/"+itoa(aliceSub.ID)+"/re-enable", "", cookies)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("re-enable active status = %d, want 409", resp.StatusCode)
	}

	// Unsubscribe.
	resp, _ = do(t, baseURL, "POST", "/api/me/subscriptions/"+itoa(aliceSub.ID)+"/unsubscribe", "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unsubscribe status = %d, want 200", resp.StatusCode)
	}
	if _, err := st.GetSubscriptionByID(context.Background(), aliceSub.ID); err == nil {
		t.Fatalf("subscription still exists after unsubscribe")
	}
}

func TestSelfServiceCannotTouchOthers(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	addMember(t, st, l, "alice@example.com")
	_, bobSub := addMember(t, st, l, "bob@example.com")
	cookies := login(t, st, baseURL, "alice@example.com")

	resp, _ := do(t, baseURL, "POST", "/api/me/subscriptions/"+itoa(bobSub.ID)+"/delivery", `{"mode":"nomail"}`, cookies)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("touching another's subscription status = %d, want 404", resp.StatusCode)
	}
}

func TestArchivesMembersOnly(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	addMember(t, st, l, "alice@example.com")

	ctx := context.Background()
	st.ArchiveMessage(ctx, l.ID, "m1", "Hello world", "alice@example.com", []byte("first post about go"), "t1")
	st.ArchiveMessage(ctx, l.ID, "m2", "Re: Hello world", "bob@example.com", []byte("second post about rust"), "t1")

	// Anonymous is rejected.
	resp, _ := do(t, baseURL, "GET", "/api/lists/example.com/dev/archives", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous archives status = %d, want 401", resp.StatusCode)
	}

	// A subscriber who is not a member of this list is rejected.
	addMember(t, st, l, "carol@example.com")
	st.DeleteSubscription(ctx, l.ID, mustSubscriber(t, st, "carol@example.com").ID)
	carolCookies := login(t, st, baseURL, "carol@example.com")
	resp, _ = do(t, baseURL, "GET", "/api/lists/example.com/dev/archives", "", carolCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-member archives status = %d, want 403", resp.StatusCode)
	}

	// A member sees the flat list.
	aliceCookies := login(t, st, baseURL, "alice@example.com")
	resp, body := do(t, baseURL, "GET", "/api/lists/example.com/dev/archives", "", aliceCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member archives status = %d, want 200", resp.StatusCode)
	}
	var entries []struct {
		ID        int64  `json:"id"`
		Subject   string `json:"subject"`
		ThreadID  string `json:"thread_id"`
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		t.Fatalf("unmarshal archives: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("archives = %d entries, want 2", len(entries))
	}
	if entries[0].ThreadID != "t1" {
		t.Fatalf("entry thread_id = %q, want t1", entries[0].ThreadID)
	}

	// Full-text search.
	resp, body = do(t, baseURL, "GET", "/api/lists/example.com/dev/archives?q=rust", "", aliceCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status = %d, want 200", resp.StatusCode)
	}
	entries = entries[:0]
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		t.Fatalf("unmarshal search: %v", err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Subject, "Re:") {
		t.Fatalf("search results = %+v, want the rust message only", entries)
	}

	// Single entry with body.
	resp, body = do(t, baseURL, "GET", "/api/lists/example.com/dev/archives/"+itoa(int64(entries[0].ID)), "", aliceCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("entry status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "rust") {
		t.Fatalf("entry body missing content: %s", body)
	}
}

func TestLogout(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	addMember(t, st, l, "alice@example.com")
	cookies := login(t, st, baseURL, "alice@example.com")

	resp, _ := do(t, baseURL, "GET", "/api/me", "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me before logout = %d, want 200", resp.StatusCode)
	}

	resp, _ = do(t, baseURL, "POST", "/api/auth/logout", "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", resp.StatusCode)
	}

	// The old session cookie no longer authenticates.
	resp, _ = do(t, baseURL, "GET", "/api/me", "", cookies)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/api/me after logout = %d, want 401", resp.StatusCode)
	}
}

func mustSubscriber(t *testing.T, st *sqlite.Store, email string) *model.Subscriber {
	t.Helper()
	sub, err := st.GetSubscriber(context.Background(), email)
	if err != nil {
		t.Fatalf("get subscriber %s: %v", email, err)
	}
	return sub
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

// TestSPAServing verifies the embedded SPA is served with an index.html
// fallback for client-side routes, while /api/ and /health still hit the API.
func TestSPAServing(t *testing.T) {
	web := fstest.MapFS{
		"web/build/index.html":  {Data: []byte("<html>spa shell</html>")},
		"web/build/robots.txt":  {Data: []byte("User-agent: *")},
		"web/build/_app/a.js":   {Data: []byte("console.log('asset')")},
	}

	st, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	if _, err := st.CreateDomain(ctx, "example.com", "Example"); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	d, _ := st.GetDomain(ctx, "example.com")
	if _, err := st.CreateList(ctx, "dev", d.ID, "example.com", "Dev list", model.ListTypeDiscussion); err != nil {
		t.Fatalf("create list: %v", err)
	}

	cfg := &config.Config{Web: config.WebConfig{BaseURL: "http://test.local"}}
	pipeline := &xmail.Pipeline{Store: st, WebBaseURL: cfg.Web.BaseURL}
	srv := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), pipeline, web)

	cases := []struct{ path, want string }{
		{"/", "spa shell"},
		{"/l/dev@example.com", "spa shell"}, // client-side route -> fallback
		{"/auth", "spa shell"},
		{"/robots.txt", "User-agent: *"},
		{"/_app/a.js", "asset"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", c.path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: body = %q, want to contain %q", c.path, rec.Body.String(), c.want)
		}
	}

	// API routes still win over the SPA.
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/lists", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "dev@example.com") {
		t.Errorf("/api/lists: status=%d body=%q", rec.Code, rec.Body.String())
	}
}
