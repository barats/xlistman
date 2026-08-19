package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"

	"github.com/barats/xlistman/internal/model"
)

// ModifyMessageOptions controls how a message is modified before delivery.
type ModifyMessageOptions struct {
	List           model.List
	WebBaseURL     string // e.g., "https://lists.example.com"
	UnsubscribeURL string // one-click unsubscribe URL for the recipient
}

// ModifyMessage applies list-specific modifications to a raw RFC 822 message:
//   - Adds standard List-* headers (always)
//   - Adds subject prefix if enabled (e.g., "[dev]")
//   - Appends footer if enabled
//   - Rewrites From header for DMARC-protected sender domains
//
// Returns the modified raw message bytes.
func ModifyMessage(raw []byte, opts ModifyMessageOptions) ([]byte, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}

	body, err := io.ReadAll(msg.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// Modify headers.
	headers := msg.Header

	// Subject prefix.
	if opts.List.Settings.SubjectPrefix != "" {
		subject := headers.Get("Subject")
		if !strings.HasPrefix(subject, opts.List.Settings.SubjectPrefix) {
			setHeader(headers, "Subject", opts.List.Settings.SubjectPrefix+" "+subject)
		}
	}

	// List-* headers (RFC 2369, RFC 2919).
	listAddr := opts.List.Address()
	listID := fmt.Sprintf("<%s.%s>", opts.List.ListName, opts.List.Domain)
	setHeader(headers, "List-Id", listID)
	setHeader(headers, "List-Post", fmt.Sprintf("<mailto:%s>", listAddr))
	setHeader(headers, "List-Help", fmt.Sprintf("<mailto:%s-request@%s?subject=help>", opts.List.ListName, opts.List.Domain))
	setHeader(headers, "List-Subscribe", fmt.Sprintf("<mailto:%s-subscribe@%s>", opts.List.ListName, opts.List.Domain))
	setHeader(headers, "List-Unsubscribe", fmt.Sprintf("<mailto:%s-unsubscribe@%s>, <%s>", opts.List.ListName, opts.List.Domain, opts.UnsubscribeURL))
	setHeader(headers, "List-Unsubscribe-Post", "List-Unsubscribe=One-Click")
	setHeader(headers, "List-Archive", fmt.Sprintf("<%s/l/%s>", opts.WebBaseURL, listAddr))

	// Footer.
	if opts.List.Settings.FooterEnabled {
		footer := fmt.Sprintf("\n\n-- \nTo unsubscribe, visit %s\nYou are subscribed to %s\n", opts.UnsubscribeURL, listAddr)
		body = appendMIMEFooter(headers, body, footer)
	}

	// Re-serialize.
	var out bytes.Buffer
	writeHeaders(&out, headers)
	out.Write(body)
	return out.Bytes(), nil
}

// setHeader sets a header value, replacing any existing values.
func setHeader(h mail.Header, key, value string) {
	h[key] = []string{value}
}

// appendMIMEFooter appends the footer to the message body without corrupting
// its MIME structure (ADR 0026): single-part text bodies are decoded,
// extended, and re-encoded; multipart messages gain a trailing text/plain
// part (or are wrapped in multipart/mixed when their top-level type cannot
// carry one).
func appendMIMEFooter(headers mail.Header, body []byte, footer string) []byte {
	contentType := headers.Get("Content-Type")
	mediaType, params := parseContentType(contentType)

	if !strings.HasPrefix(mediaType, "multipart/") {
		return appendSinglePartFooter(headers, body, footer)
	}
	return addFooterPart(headers, contentType, mediaType, params, body, footer)
}

// appendSinglePartFooter extends a non-multipart body, re-encoding it when
// the original was transfer-encoded so the footer isn't mangled.
func appendSinglePartFooter(headers mail.Header, body []byte, footer string) []byte {
	enc := strings.ToLower(strings.TrimSpace(headers.Get("Content-Transfer-Encoding")))
	switch enc {
	case "base64":
		if dec, err := base64.StdEncoding.DecodeString(string(body)); err == nil {
			updated := append(dec, []byte(footer)...)
			return []byte(base64.StdEncoding.EncodeToString(updated))
		}
	case "quoted-printable":
		if dec, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body))); err == nil {
			updated := append(dec, []byte(footer)...)
			var sb strings.Builder
			qp := quotedprintable.NewWriter(&sb)
			qp.Write(updated)
			qp.Close()
			return []byte(sb.String())
		}
	}
	return append(body, []byte(footer)...)
}

// addFooterPart rebuilds a multipart message with a trailing text/plain
// footer part. For multipart/mixed (and digest) the existing parts are copied
// verbatim and the footer appended; for any other multipart type the original
// is wrapped as a single part inside a new multipart/mixed.
func addFooterPart(headers mail.Header, contentType, mediaType string, params map[string]string, body []byte, footer string) []byte {
	var out bytes.Buffer
	mw := multipart.NewWriter(&out)

	if mediaType == "multipart/mixed" || mediaType == "multipart/digest" {
		if boundary := params["boundary"]; boundary != "" {
			mr := multipart.NewReader(bytes.NewReader(body), boundary)
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				pw, err := mw.CreatePart(part.Header)
				if err != nil {
					continue
				}
				io.Copy(pw, part)
			}
		}
	} else {
		ph := textproto.MIMEHeader{"Content-Type": []string{contentType}}
		pw, _ := mw.CreatePart(ph)
		pw.Write(body)
	}

	fp, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type": []string{"text/plain; charset=utf-8"},
	})
	fp.Write([]byte(footer))
	mw.Close()

	setHeader(headers, "Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	return out.Bytes()
}

func parseContentType(ct string) (string, map[string]string) {
	if ct == "" {
		return "", nil
	}
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return "", nil
	}
	return mt, params
}

// writeHeaders writes mail headers to the output buffer in order.
func writeHeaders(out *bytes.Buffer, headers mail.Header) {
	// Write key headers in a deterministic order, then remaining ones.
	priorityKeys := []string{
		"Return-Path", "Delivered-To", "Received", "From", "To", "Cc",
		"Subject", "Date", "Message-ID", "In-Reply-To", "References",
		"List-Id", "List-Post", "List-Help", "List-Subscribe",
		"List-Unsubscribe", "List-Unsubscribe-Post", "List-Archive",
	}

	written := make(map[string]bool)
	for _, key := range priorityKeys {
		vals := headers[key]
		if len(vals) == 0 {
			continue
		}
		for _, v := range vals {
			fmt.Fprintf(out, "%s: %s\r\n", key, v)
		}
		written[key] = true
	}

	// Write remaining headers.
	for key, vals := range headers {
		if written[key] {
			continue
		}
		for _, v := range vals {
			fmt.Fprintf(out, "%s: %s\r\n", key, v)
		}
	}
	out.WriteString("\r\n")
}

// ParseMessage extracts sender and subject from a raw RFC 822 message.
func ParseMessage(raw []byte) (sender, subject string, err error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", "", fmt.Errorf("parse message: %w", err)
	}
	from := msg.Header.Get("From")
	if addr, err := mail.ParseAddress(from); err == nil {
		sender = addr.Address
	} else {
		sender = from
	}
	subject = msg.Header.Get("Subject")
	return sender, subject, nil
}

// ScanHeaders is a helper that reads headers from a byte slice using bufio.
func ScanHeaders(raw []byte) (mail.Header, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return msg.Header, nil
}
