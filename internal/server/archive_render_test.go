package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

// structuredPost builds a raw multipart post with text+html, one attachment,
// and one nested message (ADR 0026).
func structuredPost(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part := func(h textproto.MIMEHeader, data string) {
		p, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		p.Write([]byte(data))
	}
	part(textproto.MIMEHeader{"Content-Type": []string{"text/plain; charset=utf-8"}}, body)
	part(textproto.MIMEHeader{"Content-Type": []string{"text/html; charset=utf-8"}}, "<p><b>"+body+"</b> in html</p>")
	part(textproto.MIMEHeader{
		"Content-Type":        []string{"application/octet-stream"},
		"Content-Disposition": []string{`attachment; filename="data.bin"`},
	}, "BINARY-CONTENT")
	part(textproto.MIMEHeader{"Content-Type": []string{"message/rfc822"}},
		"From: carol@example.com\r\nSubject: inner\r\n\r\nnested body text")
	mw.Close()
	head := "From: alice@example.com\r\nTo: dev@example.com\r\nSubject: structured\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"" + mw.Boundary() + "\"\r\n\r\n"
	return append([]byte(head), buf.Bytes()...)
}

func TestArchiveRenderStructuredAndAttachmentDownload(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	addMember(t, st, l, "alice@example.com")
	ctx := context.Background()

	raw := structuredPost(t, "hello plain body")
	st.ArchiveMessage(ctx, l.ID, "m1", "structured", "alice@example.com", raw, "t1", "hello plain body")
	entries, _ := st.ListArchive(ctx, l.ID, 10, 0)
	id := entries[0].ID

	cookies := login(t, st, baseURL, "alice@example.com")

	// Detail returns the structured view, not raw MIME.
	resp, body := do(t, baseURL, "GET", "/api/lists/example.com/dev/archives/"+itoa(id), "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("entry status = %d, want 200", resp.StatusCode)
	}
	var msg struct {
		Body struct {
			Text   *string `json:"text"`
			Nested []struct {
				Subject string `json:"subject"`
			} `json:"nested"`
			Attachments []struct {
				Ordinal int    `json:"ordinal"`
				Name    string `json:"name"`
			} `json:"attachments"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatalf("unmarshal entry: %v", err)
	}
	if msg.Body.Text == nil || *msg.Body.Text != "hello plain body" {
		t.Errorf("structured text = %v, want hello plain body", msg.Body.Text)
	}
	if len(msg.Body.Attachments) != 1 || msg.Body.Attachments[0].Name != "data.bin" {
		t.Errorf("structured attachments = %+v", msg.Body.Attachments)
	}
	if len(msg.Body.Nested) != 1 || msg.Body.Nested[0].Subject != "inner" {
		t.Errorf("structured nested = %+v", msg.Body.Nested)
	}

	// Attachment download: member can fetch the decoded bytes.
	ord := msg.Body.Attachments[0].Ordinal
	resp, abody := do(t, baseURL, "GET", "/api/lists/example.com/dev/archives/"+itoa(id)+"/attachments/"+itoa(int64(ord)), "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("attachment status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(abody, "BINARY-CONTENT") {
		t.Errorf("attachment body = %q", abody)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "data.bin") {
		t.Errorf("content-disposition = %q", cd)
	}

	// Anonymous cannot download.
	anonymResp, _ := do(t, baseURL, "GET", "/api/lists/example.com/dev/archives/"+itoa(id)+"/attachments/"+itoa(int64(ord)), "", nil)
	if anonymResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous attachment status = %d, want 401", anonymResp.StatusCode)
	}

	// Unknown ordinal is 404.
	resp, _ = do(t, baseURL, "GET", "/api/lists/example.com/dev/archives/"+itoa(id)+"/attachments/99", "", cookies)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("bad ordinal status = %d, want 404", resp.StatusCode)
	}
}

func TestHeldDetailRenderedStructured(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	admin, _ := st.GetOrCreateSubscriber(context.Background(), "admin@example.com")
	st.AddOwner(context.Background(), l.ID, admin.ID)

	raw := structuredPost(t, "held body text")
	held, err := st.CreateHeldMessage(context.Background(), l.ID, "charlie@example.com", "held post", raw, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	cookies := login(t, st, baseURL, "admin@example.com")
	resp, body := do(t, baseURL, "GET", "/api/console/lists/example.com/dev/held/"+itoa(held.ID), "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("held detail status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, `"nested"`) || !strings.Contains(body, `"attachments"`) || !strings.Contains(body, "held body text") {
		t.Errorf("held detail not structured: %s", body)
	}

	// The held attachment is downloadable to the moderator.
	resp, abody := do(t, baseURL, "GET", "/api/console/lists/example.com/dev/held/"+itoa(held.ID)+"/attachments/0", "", cookies)
	if resp.StatusCode != http.StatusOK || !strings.Contains(abody, "BINARY-CONTENT") {
		t.Errorf("held attachment status = %d body = %q", resp.StatusCode, abody)
	}
}

func TestSettingsRejectNegativeAttachmentSize(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	admin, _ := st.GetOrCreateSubscriber(context.Background(), "admin@example.com")
	st.AddOwner(context.Background(), l.ID, admin.ID)
	cookies := login(t, st, baseURL, "admin@example.com")

	body := `{"description":"","settings":{"max_attachment_size":-1}}`
	resp, _ := do(t, baseURL, "PUT", "/api/console/lists/example.com/dev/settings", body, cookies)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("negative max_attachment_size status = %d, want 400", resp.StatusCode)
	}
}

func TestListInstructionsRoundTrip(t *testing.T) {
	_, st, baseURL := newTestServer(t)
	l := setupList(t, st)
	admin, _ := st.GetOrCreateSubscriber(context.Background(), "admin@example.com")
	st.AddOwner(context.Background(), l.ID, admin.ID)
	cookies := login(t, st, baseURL, "admin@example.com")

	// An owner saves multi-line instructions.
	resp, _ := do(t, baseURL, "PUT", "/api/console/lists/example.com/dev/settings",
		`{"description":"Dev","instructions":"Line one\nLine two","settings":{}}`, cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save instructions status = %d, want 200", resp.StatusCode)
	}

	// The public list info exposes them (no auth needed to read).
	resp, body := do(t, baseURL, "GET", "/api/lists/example.com/dev", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public info status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, `"instructions":"Line one\nLine two"`) {
		t.Errorf("public info missing instructions: %s", body)
	}

	// The console settings echo them back to the owner.
	resp, body = do(t, baseURL, "GET", "/api/console/lists/example.com/dev/settings", "", cookies)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"instructions":"Line one\nLine two"`) {
		t.Errorf("console settings missing instructions: %s", body)
	}
}
