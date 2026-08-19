package mail

import (
	"bytes"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"

	"github.com/barats/xlistman/internal/mailparse"
	"github.com/barats/xlistman/internal/model"
)

func TestModifyMessageFooterMultipart(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	p1, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": []string{"text/plain; charset=utf-8"}})
	p1.Write([]byte("hello plain body"))
	p2, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":        []string{"application/octet-stream"},
		"Content-Disposition": []string{`attachment; filename="report.pdf"`},
	})
	p2.Write([]byte("%PDF"))
	mw.Close()
	raw := append([]byte("From: a@b\r\nTo: dev@example.com\r\nSubject: hi\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\""+mw.Boundary()+"\"\r\n\r\n"), buf.Bytes()...)

	l := model.List{ListName: "dev", Domain: "example.com", Settings: model.DefaultListSettings(model.ListTypeDiscussion)}
	l.Settings.FooterEnabled = true
	out, err := ModifyMessage(raw, ModifyMessageOptions{List: l, WebBaseURL: "https://lists.example.com", UnsubscribeURL: "https://lists.example.com/unsubscribe?x=1"})
	if err != nil {
		t.Fatalf("ModifyMessage: %v", err)
	}

	// The result must still parse, keep the attachment, and carry the footer
	// as a part (not appended outside the MIME tree).
	p, err := mailparse.ParseMessageMIME(out)
	if err != nil {
		t.Fatalf("modified message no longer parses: %v", err)
	}
	if p.Text == nil || *p.Text != "hello plain body" {
		t.Errorf("modified text = %v", p.Text)
	}
	if len(p.Attachments) != 1 || p.Attachments[0].Name != "report.pdf" {
		t.Errorf("attachment lost by footer: %+v", p.Attachments)
	}
	if !strings.Contains(string(out), "To unsubscribe, visit") {
		t.Errorf("footer missing: %s", out)
	}
	if !strings.Contains(string(out), "multipart/mixed") {
		t.Errorf("modified message is not multipart/mixed: %s", out)
	}
}

func TestModifyMessageFooterSinglePart(t *testing.T) {
	l := model.List{ListName: "dev", Domain: "example.com", Settings: model.DefaultListSettings(model.ListTypeDiscussion)}
	l.Settings.FooterEnabled = true
	raw := []byte("From: a@b\r\nTo: dev@example.com\r\nSubject: hi\r\n\r\nhello")
	out, err := ModifyMessage(raw, ModifyMessageOptions{List: l, WebBaseURL: "https://lists.example.com", UnsubscribeURL: "https://u.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "hello\n\n-- \nTo unsubscribe") {
		t.Errorf("single-part footer not appended: %s", out)
	}
}

func TestModifyMessageFooterAlternativeWrapped(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	p1, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": []string{"text/plain; charset=utf-8"}})
	p1.Write([]byte("plain alt"))
	p2, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": []string{"text/html; charset=utf-8"}})
	p2.Write([]byte("<p>html alt</p>"))
	mw.Close()
	raw := append([]byte("From: a@b\r\nTo: dev@example.com\r\nSubject: alt\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\""+mw.Boundary()+"\"\r\n\r\n"), buf.Bytes()...)

	l := model.List{ListName: "dev", Domain: "example.com", Settings: model.DefaultListSettings(model.ListTypeDiscussion)}
	l.Settings.FooterEnabled = true
	out, err := ModifyMessage(raw, ModifyMessageOptions{List: l, WebBaseURL: "https://lists.example.com", UnsubscribeURL: "https://u.example.com"})
	if err != nil {
		t.Fatalf("ModifyMessage: %v", err)
	}
	p, err := mailparse.ParseMessageMIME(out)
	if err != nil {
		t.Fatalf("wrapped message no longer parses: %v", err)
	}
	if p.Text == nil || *p.Text != "plain alt" {
		t.Errorf("alternative text lost: %v", p.Text)
	}
	if !strings.Contains(string(out), "To unsubscribe, visit") {
		t.Errorf("footer missing: %s", out)
	}
}
