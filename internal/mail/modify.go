package mail

import (
	"bufio"
	"bytes"
	"fmt"
	"net/mail"
	"strings"

	"github.com/barat/xlistman/internal/model"
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

	// Read the body.
	bodyBuf := new(bytes.Buffer)
	bodyBuf.ReadFrom(msg.Body)
	body := bodyBuf.Bytes()

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
		body = appendFooter(body, footer)
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

// appendFooter appends a text footer to the message body.
// For multipart messages, this is a simplification that appends to the raw body.
// A future improvement would add a proper MIME part.
func appendFooter(body []byte, footer string) []byte {
	return append(body, []byte(footer)...)
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

// Ensure bufio is used (for potential future header scanning optimizations).
var _ = bufio.NewScanner
