package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/barats/xlistman/internal/model"
)

// doMultipart POSTs a single-file multipart form (Phase 14 import).
func doMultipart(t *testing.T, baseURL, path, filename, content string, cookies []*http.Cookie) (*http.Response, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("write form: %v", err)
	}
	mw.Close()
	req, err := http.NewRequest(http.MethodPost, baseURL+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, string(b)
}

func TestConsoleMembersExport(t *testing.T) {
	_, st, baseURL, _, _ := consoleFixture(t)
	owner := makeSubscriber(t, st, "owner@example.com")
	cookies := login(t, st, baseURL, owner.Email)

	resp, body := do(t, baseURL, "GET", "/api/console/lists/example.com/dev/members/export", "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("content-type = %q, want text/csv", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("content-disposition = %q, want an attachment", cd)
	}
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) < 2 {
		t.Fatalf("export lines = %d, want header + members; body=%q", len(lines), body)
	}
	if lines[0] != "email,status,delivery_mode,roles" {
		t.Errorf("header = %q, want %q", lines[0], "email,status,delivery_mode,roles")
	}
	// alice is an Active member; owner and mod are role holders without a
	// subscription, and must still appear (sorted by email).
	found := map[string]bool{}
	for _, ln := range lines[1:] {
		found[strings.Split(ln, ",")[0]] = true
	}
	for _, want := range []string{"alice@example.com", "mod@example.com", "owner@example.com"} {
		if !found[want] {
			t.Errorf("export missing %s; body=%q", want, body)
		}
	}
	if !strings.Contains(body, "active,regular,") {
		t.Errorf("export should show alice as active/regular; body=%q", body)
	}
}

func TestConsoleMembersExportGated(t *testing.T) {
	_, st, baseURL, _, _ := consoleFixture(t)

	// Unauthenticated → 401.
	resp, _ := do(t, baseURL, "GET", "/api/console/lists/example.com/dev/members/export", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated export status = %d, want 401", resp.StatusCode)
	}

	// Moderator (not owner) → 403.
	mod := makeSubscriber(t, st, "mod@example.com")
	modCookies := login(t, st, baseURL, mod.Email)
	resp, _ = do(t, baseURL, "GET", "/api/console/lists/example.com/dev/members/export", "", modCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("moderator export status = %d, want 403", resp.StatusCode)
	}
}

func TestConsoleMembersImport(t *testing.T) {
	_, st, baseURL, disc, _ := consoleFixture(t)
	owner := makeSubscriber(t, st, "owner@example.com")
	cookies := login(t, st, baseURL, owner.Email)

	csv := "email,status,delivery_mode,roles\nnewone@example.com,active,regular,\nnewtwo@example.com\n"
	resp, body := doMultipart(t, baseURL, "/api/console/lists/example.com/dev/members/import", "members.csv", csv, cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["added"] != float64(2) {
		t.Errorf("added = %v, want 2", got["added"])
	}

	// The imported addresses are Active members.
	for _, email := range []string{"newone@example.com", "newtwo@example.com"} {
		sub, err := st.GetSubscriber(context.Background(), email)
		if err != nil {
			t.Fatalf("GetSubscriber(%s): %v", email, err)
		}
		subscr, err := st.GetSubscription(context.Background(), disc.ID, sub.ID)
		if err != nil {
			t.Fatalf("GetSubscription(%s): %v", email, err)
		}
		if subscr.Status != model.SubscriptionStatusActive {
			t.Errorf("%s status = %q, want active", email, subscr.Status)
		}
	}

	// One member.import Audit Event with the owner as actor.
	events, err := st.ListAuditEvents(context.Background(), &disc.ID, model.ActionMemberImport, 0)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("member.import events = %d, want 1", len(events))
	}
	if events[0].ActorEmail != owner.Email || events[0].Detail != "added 2, skipped 0" {
		t.Errorf("audit event = %+v, want actor %s and detail %q", events[0], owner.Email, "added 2, skipped 0")
	}
}

func TestConsoleMembersImportSkipsAndReports(t *testing.T) {
	_, st, baseURL, _, _ := consoleFixture(t)
	owner := makeSubscriber(t, st, "owner@example.com")
	cookies := login(t, st, baseURL, owner.Email)

	// alice already exists; include an invalid row and a duplicate.
	csv := "email\n" +
		"alice@example.com\n" + // already subscribed → skipped
		"brand@example.com\n" + // new
		"not-an-email\n" + // invalid
		"brand@example.com\n" // duplicate of the new row → already
	resp, body := doMultipart(t, baseURL, "/api/console/lists/example.com/dev/members/import", "m.csv", csv, cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["added"] != float64(1) {
		t.Errorf("added = %v, want 1", got["added"])
	}
	if got["already"] != float64(2) {
		t.Errorf("already = %v, want 2", got["already"])
	}
	if got["invalid"] != float64(1) {
		t.Errorf("invalid = %v, want 1", got["invalid"])
	}
}

func TestConsoleMembersImportGatedAndErrors(t *testing.T) {
	_, st, baseURL, _, _ := consoleFixture(t)

	// Unauthenticated → 401.
	resp, _ := doMultipart(t, baseURL, "/api/console/lists/example.com/dev/members/import", "m.csv", "a@example.com\n", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated import status = %d, want 401", resp.StatusCode)
	}

	// Moderator (not owner) → 403.
	mod := makeSubscriber(t, st, "mod@example.com")
	modCookies := login(t, st, baseURL, mod.Email)
	resp, _ = doMultipart(t, baseURL, "/api/console/lists/example.com/dev/members/import", "m.csv", "a@example.com\n", modCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("moderator import status = %d, want 403", resp.StatusCode)
	}

	// Owner but malformed CSV → 400, nothing imported.
	owner := makeSubscriber(t, st, "owner@example.com")
	ownerCookies := login(t, st, baseURL, owner.Email)
	resp, _ = doMultipart(t, baseURL, "/api/console/lists/example.com/dev/members/import", "bad.csv", "\"unterminated\n", ownerCookies)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed import status = %d, want 400", resp.StatusCode)
	}
	if _, err := st.GetSubscriber(context.Background(), "a@example.com"); err == nil {
		t.Errorf("malformed import should not have imported anything")
	}
}
