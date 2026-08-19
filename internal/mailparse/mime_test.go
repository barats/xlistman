package mailparse

import (
	"bytes"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"

	"github.com/barats/xlistman/internal/model"
)

// buildMultipart assembles a raw multipart/mixed message with a text/plain
// body, a text/html body, one attachment, and one nested message.
func buildMultipart(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	part := func(ct, body string, extra ...string) {
		headers := textproto.MIMEHeader{"Content-Type": []string{ct}}
		for i := 0; i+1 < len(extra); i += 2 {
			headers[extra[i]] = []string{extra[i+1]}
		}
		p, err := mw.CreatePart(headers)
		if err != nil {
			t.Fatal(err)
		}
		p.Write([]byte(body))
	}

	part("text/plain; charset=utf-8", "hello plain body")
	part("text/html; charset=utf-8", "<p>hello <b>html</b> body</p>")
	part("application/pdf", "%PDF-1.4 fake", "Content-Disposition", `attachment; filename="report.pdf"`)

	nested := []byte("From: carol@example.com\r\nSubject: inner subject\r\nDate: Mon, 1 Jan 2024 00:00:00 +0000\r\n\r\nnested body\r\n")
	part("message/rfc822", string(nested))
	mw.Close()

	head := "From: alice@example.com\r\nTo: dev@example.com\r\nSubject: multipart test\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"" + mw.Boundary() + "\"\r\n\r\n"
	return append([]byte(head), buf.Bytes()...)
}

func TestParseMessageMIME(t *testing.T) {
	raw := buildMultipart(t)
	p, err := ParseMessageMIME(raw)
	if err != nil {
		t.Fatalf("ParseMessageMIME: %v", err)
	}

	if p.Text == nil || *p.Text != "hello plain body" {
		t.Errorf("Text = %v, want plain body", p.Text)
	}
	if p.HTML == nil || !strings.Contains(*p.HTML, "<b>html</b>") {
		t.Errorf("HTML = %v, want html body", p.HTML)
	}
	if p.From != "alice@example.com" || p.Subject != "multipart test" {
		t.Errorf("From/Subject = %q/%q", p.From, p.Subject)
	}

	if len(p.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(p.Attachments))
	}
	a := p.Attachments[0]
	if a.Name != "report.pdf" || a.ContentType != "application/pdf" || a.Size == 0 {
		t.Errorf("attachment = %+v", a)
	}
	if a.Ordinal != 0 {
		t.Errorf("attachment ordinal = %d, want 0", a.Ordinal)
	}

	if len(p.Nested) != 1 {
		t.Fatalf("nested = %d, want 1", len(p.Nested))
	}
	n := p.Nested[0]
	if n.From != "carol@example.com" || n.Subject != "inner subject" {
		t.Errorf("nested from/subject = %q/%q", n.From, n.Subject)
	}
	if n.Text == nil || *n.Text != "nested body" {
		t.Errorf("nested text = %v", n.Text)
	}
	if got := p.AttachmentByOrdinal(a.Ordinal); got != a {
		t.Error("AttachmentByOrdinal did not return the attachment")
	}
}

// A text/plain part declared as an attachment is an Attachment, not a body
// part (ADR 0025) — e.g. an attached .txt file.
func TestParseMessageTextPlainAttachment(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	p1, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": []string{"text/plain; charset=utf-8"}})
	p1.Write([]byte("the real body"))
	p2, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":        []string{"text/plain; charset=utf-8"},
		"Content-Disposition": []string{`attachment; filename="notes.txt"`},
	})
	p2.Write([]byte("attached notes content"))
	mw.Close()
	raw := append([]byte("From: a@b\r\nSubject: x\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\""+mw.Boundary()+"\"\r\n\r\n"), buf.Bytes()...)

	p, err := ParseMessageMIME(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Text == nil || *p.Text != "the real body" {
		t.Errorf("body text = %v, want the real body", p.Text)
	}
	if len(p.Attachments) != 1 || p.Attachments[0].Name != "notes.txt" || p.Attachments[0].Size == 0 {
		t.Fatalf("text/plain attachment not listed: %+v", p.Attachments)
	}
	if string(p.Attachments[0].Bytes) != "attached notes content" {
		t.Errorf("attachment bytes = %q", p.Attachments[0].Bytes)
	}
}

func TestParseMessageMIMEBase64Attachment(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	p1, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              []string{"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": []string{"base64"},
	})
	p1.Write([]byte("cGxhaW4=")) // "plain" base64
	p2, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              []string{"image/png"},
		"Content-Disposition":       []string{`inline; filename="logo.png"`},
		"Content-ID":                []string{"<logo@example.com>"},
		"Content-Transfer-Encoding": []string{"base64"},
	})
	p2.Write([]byte("iVBORw0KGgo=")) // fake png bytes
	mw.Close()

	raw := append([]byte("From: a@b\r\nSubject: x\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\""+mw.Boundary()+"\"\r\n\r\n"), buf.Bytes()...)
	p, err := ParseMessageMIME(raw)
	if err != nil {
		t.Fatalf("ParseMessageMIME: %v", err)
	}
	if p.Text == nil || *p.Text != "plain" {
		t.Errorf("base64 text = %v, want decoded 'plain'", p.Text)
	}
	if len(p.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(p.Attachments))
	}
	a := p.Attachments[0]
	if !a.Inline || a.ContentID != "logo@example.com" {
		t.Errorf("inline attachment = %+v", a)
	}
	if a.Size != 8 { // len("iVBORw0KGgo=") decoded
		t.Errorf("attachment size = %d, want 8 (decoded)", a.Size)
	}
}

func TestParseMessageHTMLSanitized(t *testing.T) {
	raw := []byte("From: a@b\r\nSubject: x\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p onclick=\"evil()\">hi</p><script>alert(1)</script><a href=\"http://x.test\">link</a>")
	p, err := ParseMessageMIME(raw)
	if err != nil {
		t.Fatal(err)
	}
	html := *p.HTML
	if strings.Contains(html, "script") || strings.Contains(html, "onclick") {
		t.Errorf("html not sanitized: %q", html)
	}
	if !strings.Contains(html, `href="http://x.test"`) {
		t.Errorf("safe link removed: %q", html)
	}
}

func TestExtractText(t *testing.T) {
	raw := buildMultipart(t)
	text := ExtractText(raw)
	if !strings.Contains(text, "hello plain body") {
		t.Errorf("extract text missing plain body: %q", text)
	}
	// HTML body reduced to text, attachment content excluded.
	if strings.Contains(text, "%PDF") || strings.Contains(text, "multipart") {
		t.Errorf("extract text includes MIME/attachment noise: %q", text)
	}

	htmlRaw := []byte("From: a@b\r\nSubject: x\r\nMIME-Version: 1.0\r\nContent-Type: text/html\r\n\r\n<p>Only <b>html</b> here</p>")
	if text := ExtractText(htmlRaw); !strings.Contains(text, "Only html here") {
		t.Errorf("html extraction = %q", text)
	}
}

func TestValidatePostPolicy(t *testing.T) {
	settings := func(mut func(*model.ListSettings)) *model.List {
		l := &model.List{ListName: "dev", Domain: "example.com", Settings: model.DefaultListSettings(model.ListTypeDiscussion)}
		mut(&l.Settings)
		return l
	}

	withAtt := buildMultipart(t)
	small := []byte("From: a@b\r\nSubject: x\r\n\r\nsmall")

	if reason := ValidatePostPolicy(settings(func(s *model.ListSettings) {}), small); reason != "" {
		t.Errorf("default list rejected a plain post: %q", reason)
	}
	if reason := ValidatePostPolicy(settings(func(s *model.ListSettings) {}), withAtt); reason != "" {
		t.Errorf("default list rejected an attachment post: %q", reason)
	}

	noAtt := settings(func(s *model.ListSettings) { s.AllowAttachments = false })
	if reason := ValidatePostPolicy(noAtt, withAtt); !strings.Contains(reason, "does not allow attachments") {
		t.Errorf("no-attachment list: reason = %q", reason)
	}
	if reason := ValidatePostPolicy(noAtt, small); reason != "" {
		t.Errorf("no-attachment list rejected a plain post: %q", reason)
	}

	sizeCapped := settings(func(s *model.ListSettings) { s.MaxAttachmentSize = 10 })
	if reason := ValidatePostPolicy(sizeCapped, withAtt); !strings.Contains(reason, "per-attachment size limit") {
		t.Errorf("size-capped list: reason = %q", reason)
	}

	totalCapped := settings(func(s *model.ListSettings) { s.MaxMessageSize = 100 })
	if reason := ValidatePostPolicy(totalCapped, withAtt); !strings.Contains(reason, "size limit") {
		t.Errorf("total-capped list: reason = %q", reason)
	}

	// Malformed message passes (nothing to enforce against).
	if reason := ValidatePostPolicy(noAtt, []byte("not a valid message at all")); reason != "" {
		t.Errorf("malformed message rejected: %q", reason)
	}
}
