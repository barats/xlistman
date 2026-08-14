package mail

import (
	"fmt"
	"net/smtp"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// SMTPClient sends outbound email via an MTA's SMTP relay. When Mode is
// "sink", messages are written to SinkDir instead (for development without
// an MTA), one file per message.
type SMTPClient struct {
	Host     string
	Port     int
	Username string
	Password string
	Mode     string // "smtp" (default) or "sink"
	SinkDir  string // directory for outbound mail when Mode is "sink"
}

// Send delivers a single message via SMTP to the MTA, or writes it to the
// sink directory in sink mode.
// envelopeSender is the MAIL FROM address (VERP for list posts).
// recipient is the RCPT TO address.
// rawMsg is the full RFC 822 message bytes.
func (c *SMTPClient) Send(envelopeSender, recipient string, rawMsg []byte) error {
	if c.Mode == "sink" {
		return c.sendToSink(envelopeSender, recipient, rawMsg)
	}
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	var auth smtp.Auth
	if c.Username != "" {
		auth = smtp.PlainAuth("", c.Username, c.Password, c.Host)
	}
	return smtp.SendMail(addr, auth, envelopeSender, []string{recipient}, rawMsg)
}

// sendToSink writes a message to the sink directory, named by timestamp and
// recipient, with the envelope addresses recorded in prepended headers.
func (c *SMTPClient) sendToSink(envelopeSender, recipient string, rawMsg []byte) error {
	if err := os.MkdirAll(c.SinkDir, 0o755); err != nil {
		return fmt.Errorf("create sink dir: %w", err)
	}
	name := fmt.Sprintf("%d-%s.eml", time.Now().UnixNano(), sanitizeFilename(recipient))
	path := filepath.Join(c.SinkDir, name)
	header := fmt.Sprintf("X-Envelope-From: %s\r\nX-Envelope-To: %s\r\n", envelopeSender, recipient)
	if err := os.WriteFile(path, append([]byte(header), rawMsg...), 0o644); err != nil {
		return fmt.Errorf("write sink message: %w", err)
	}
	return nil
}

var nonFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func sanitizeFilename(s string) string {
	return nonFilenameChars.ReplaceAllString(s, "_")
}
