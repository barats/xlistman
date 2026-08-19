// Package mailparse provides the MIME-aware, structured view of an email
// (ADR 0026) shared by the mail pipeline, the archive store, and the web
// API. It lives below internal/mail so the store can use it without the
// import cycle that internal/mail's tests would otherwise create.
package mailparse

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
	"path/filepath"
	"regexp"
	"strings"

	"github.com/barats/xlistman/internal/model"
	"github.com/microcosm-cc/bluemonday"
)

// ParsedMessage is the MIME-aware, structured view of an email (ADR 0026).
// It is what the archive and moderation surfaces render; the raw bytes remain
// the source of truth for delivery.
type ParsedMessage struct {
	// From, Subject, and Date are the message's own headers, used to render
	// nested (message/rfc822) messages as quoted sub-messages.
	From    string `json:"from,omitempty"`
	Subject string `json:"subject,omitempty"`
	Date    string `json:"date,omitempty"`
	// Text is the readable text/plain body, if any.
	Text *string `json:"text,omitempty"`
	// HTML is the text/html body, if any, already sanitized.
	HTML *string `json:"html,omitempty"`
	// Nested holds message/rfc822 parts (forwarded emails), rendered inline.
	Nested []*ParsedMessage `json:"nested,omitempty"`
	// Attachments are the non-text file parts of this message (ADR 0025).
	Attachments []*Attachment `json:"attachments,omitempty"`
}

// Attachment is a non-text file part of a message (ADR 0025): an attached
// file or an inline image. Ordinal addresses it across the whole message
// tree, depth-first, for the download endpoint.
type Attachment struct {
	Ordinal     int    `json:"ordinal"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	ContentID   string `json:"content_id,omitempty"`
	Inline      bool   `json:"inline"`
	Size        int    `json:"size"`
	Bytes       []byte `json:"-"`
}

// ParseMessageMIME parses raw RFC 822/MIME bytes into a structured view.
// HTML bodies are sanitized, so the result is safe to render.
func ParseMessageMIME(raw []byte) (*ParsedMessage, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	p := &ParsedMessage{}
	ordinal := 0
	parsePart(p, msg.Header, msg.Body, &ordinal)
	p.From = decodeHeader(msg.Header.Get("From"))
	p.Subject = decodeHeader(msg.Header.Get("Subject"))
	p.Date = decodeHeader(msg.Header.Get("Date"))
	return p, nil
}

// hdrGet returns the first value of a header key on a MIME header map, which
// is the shared underlying type of net/mail.Header and textproto.MIMEHeader.
func hdrGet(h map[string][]string, key string) string {
	if v := h[textproto.CanonicalMIMEHeaderKey(key)]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// parsePart folds one MIME part (or the top-level message) into p. The
// header type is the common map underlying both net/mail.Header and
// textproto.MIMEHeader (multipart.Part.Header).
func parsePart(p *ParsedMessage, header map[string][]string, body io.Reader, ordinal *int) {
	mediaType, params := mediaTypeParams(header)
	if mediaType == "" {
		mediaType = "text/plain"
	}

	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		parseMultipart(p, body, params["boundary"], ordinal)
	case mediaType == "message/rfc822":
		parseNested(p, body, ordinal)
	case isFilePart(header):
		// A part that carries a filename or an attachment disposition is an
		// Attachment even when its media type is text/* (e.g. an attached
		// .txt file) — only a filename-less text body is the body (ADR 0025).
		p.Attachments = append(p.Attachments, readAttachment(header, body, ordinal))
	case mediaType == "text/plain":
		if p.Text == nil {
			if s := decodeText(body, hdrGet(header, "Content-Transfer-Encoding"), params["charset"]); s != "" {
				p.Text = &s
			}
		}
	case mediaType == "text/html":
		if p.HTML == nil {
			if s := decodeText(body, hdrGet(header, "Content-Transfer-Encoding"), params["charset"]); s != "" {
				s = SanitizeHTML(s)
				p.HTML = &s
			}
		}
	default:
		p.Attachments = append(p.Attachments, readAttachment(header, body, ordinal))
	}
}

// isFilePart reports whether a part is an Attachment by declaration: an
// explicit attachment disposition, a filename on the disposition or content
// type, or a Content-Type name parameter (ADR 0025).
func isFilePart(header map[string][]string) bool {
	if d := hdrGet(header, "Content-Disposition"); d != "" {
		if dm, dp, err := mime.ParseMediaType(d); err == nil {
			if strings.ToLower(dm) == "attachment" || dp["filename"] != "" {
				return true
			}
		}
	}
	if _, params := mediaTypeParams(header); params["name"] != "" {
		return true
	}
	return false
}

func parseMultipart(p *ParsedMessage, body io.Reader, boundary string, ordinal *int) {
	if boundary == "" {
		return
	}
	mr := multipart.NewReader(body, boundary)
	for {
		part, err := mr.NextPart()
		if err != nil {
			return // io.EOF, or a malformed part ends the walk
		}
		parsePart(p, part.Header, part, ordinal)
	}
}

func parseNested(p *ParsedMessage, body io.Reader, ordinal *int) {
	nested := &ParsedMessage{}
	msg, err := mail.ReadMessage(body)
	if err != nil {
		return // unparseable nested message: skip it
	}
	parsePart(nested, msg.Header, msg.Body, ordinal)
	nested.From = decodeHeader(msg.Header.Get("From"))
	nested.Subject = decodeHeader(msg.Header.Get("Subject"))
	nested.Date = decodeHeader(msg.Header.Get("Date"))
	p.Nested = append(p.Nested, nested)
}

func readAttachment(header map[string][]string, body io.Reader, ordinal *int) *Attachment {
	a := &Attachment{Ordinal: *ordinal}
	*ordinal++

	mediaType, params := mediaTypeParams(header)
	a.ContentType = mediaType
	if a.ContentType == "" {
		a.ContentType = "application/octet-stream"
	}

	name := ""
	disposition := ""
	if d := hdrGet(header, "Content-Disposition"); d != "" {
		if dm, dp, err := mime.ParseMediaType(d); err == nil {
			disposition = strings.ToLower(dm)
			name = dp["filename"]
		}
	}
	if name == "" {
		name = params["name"]
	}
	a.Inline = disposition == "inline"

	// The filename is attacker-supplied: keep only the base name, drop
	// control characters, and fall back to a neutral default.
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" {
		name = "attachment"
	}
	a.Name = name

	if cid := hdrGet(header, "Content-ID"); cid != "" {
		a.ContentID = strings.Trim(cid, "<> \t")
	}

	a.Bytes = decodeBytes(body, hdrGet(header, "Content-Transfer-Encoding"))
	if a.Bytes == nil {
		a.Bytes = []byte{}
	}
	a.Size = len(a.Bytes)
	return a
}

// AttachmentByOrdinal returns the attachment with the given ordinal across
// the whole message tree, depth-first, or nil.
func (p *ParsedMessage) AttachmentByOrdinal(ordinal int) *Attachment {
	for _, a := range p.Attachments {
		if a.Ordinal == ordinal {
			return a
		}
	}
	for _, n := range p.Nested {
		if a := n.AttachmentByOrdinal(ordinal); a != nil {
			return a
		}
	}
	return nil
}

// ValidatePostPolicy checks a post against the list's size and attachment
// policy (ADR 0025). It returns an empty string when the post is acceptable,
// or a human-readable reason for rejection. Unparseable messages pass: there
// is nothing to enforce against, and the total-size cap still applies.
func ValidatePostPolicy(l *model.List, raw []byte) string {
	if l.Settings.MaxMessageSize > 0 && int64(len(raw)) > l.Settings.MaxMessageSize {
		return fmt.Sprintf("Your message exceeds this list's size limit of %d bytes.", l.Settings.MaxMessageSize)
	}
	if !l.Settings.AllowAttachments || l.Settings.MaxAttachmentSize > 0 {
		p, err := ParseMessageMIME(raw)
		if err != nil {
			return ""
		}
		for _, a := range p.Attachments {
			if !l.Settings.AllowAttachments {
				return "This list does not allow attachments."
			}
			if l.Settings.MaxAttachmentSize > 0 && int64(a.Size) > l.Settings.MaxAttachmentSize {
				return fmt.Sprintf("Attachment %q exceeds this list's per-attachment size limit of %d bytes.",
					a.Name, l.Settings.MaxAttachmentSize)
			}
		}
	}
	return ""
}

// ExtractText returns the clean, searchable plain text of a message (ADR
// 0026): text bodies and HTML-reduced-to-text, headers stripped and
// attachments skipped. On parse failure it returns the raw bytes, so search
// still has something to index.
func ExtractText(raw []byte) string {
	p, err := ParseMessageMIME(raw)
	if err != nil {
		return string(raw)
	}
	var sb strings.Builder
	p.writeText(&sb)
	return strings.TrimSpace(sb.String())
}

func (p *ParsedMessage) writeText(sb *strings.Builder) {
	if p.Text != nil {
		sb.WriteString(*p.Text)
		sb.WriteString("\n")
	} else if p.HTML != nil {
		sb.WriteString(htmlToText(*p.HTML))
		sb.WriteString("\n")
	}
	for _, n := range p.Nested {
		n.writeText(sb)
	}
}

// SanitizeHTML returns HTML with scripts, event handlers, and other unsafe
// constructs stripped, safe to render on a members-only page.
func SanitizeHTML(s string) string {
	return htmlPolicy.Sanitize(s)
}

var (
	htmlTagRe    = regexp.MustCompile(`(?s)<[^>]*>`)
	htmlEntityRe = regexp.MustCompile(`&[a-zA-Z#0-9]+;`)
	wsRe         = regexp.MustCompile(`[ \t\r\n]+`)
)

// htmlToText reduces sanitized HTML to roughly its visible text for search.
func htmlToText(h string) string {
	s := htmlTagRe.ReplaceAllString(h, " ")
	s = htmlEntityRe.ReplaceAllStringFunc(s, func(e string) string {
		switch e {
		case "&amp;":
			return "&"
		case "&lt;":
			return "<"
		case "&gt;":
			return ">"
		case "&quot;":
			return `"`
		case "&#39;", "&apos;":
			return "'"
		case "&nbsp;":
			return " "
		default:
			return e
		}
	})
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

// htmlPolicy is the bluemonday policy for rendering archived HTML. UGCPolicy
// allows standard text formatting and links; img is allowed explicitly so
// inline images (cid: or data URIs) can render, without scripts or events.
var htmlPolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("src").OnElements("img")
	p.AllowAttrs("alt", "width", "height").OnElements("img")
	p.AllowURLSchemes("http", "https", "cid", "mailto")
	p.AllowDataURIImages()
	return p
}()

// decodeHeader decodes an RFC 2047 encoded-word header value to plain text.
func decodeHeader(s string) string {
	if s == "" {
		return ""
	}
	dec := &mime.WordDecoder{}
	if d, err := dec.DecodeHeader(s); err == nil {
		return d
	}
	return s
}

func mediaTypeParams(header map[string][]string) (string, map[string]string) {
	ct := hdrGet(header, "Content-Type")
	if ct == "" {
		return "", nil
	}
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return "", nil
	}
	return mt, params
}

// decodeText decodes a text part's transfer encoding and charset to a UTF-8
// string, trimming trailing whitespace so bodies render cleanly. Unknown
// charsets pass through as-is.
func decodeText(body io.Reader, enc, charset string) string {
	data := decodeBytes(body, enc)
	if data == nil {
		return ""
	}
	var s string
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "iso-8859-1", "latin1", "latin-1", "iso8859-1":
		var sb strings.Builder
		for _, c := range data {
			sb.WriteRune(rune(c))
		}
		s = sb.String()
	default:
		s = string(data)
	}
	return strings.TrimRight(s, " \t\r\n")
}

func decodeBytes(body io.Reader, enc string) []byte {
	var r io.Reader = body
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, body)
	case "quoted-printable":
		r = quotedprintable.NewReader(body)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	return b
}
