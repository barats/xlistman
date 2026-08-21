package mail

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	// TLS selects the transport security: "none", "starttls" (opportunistic,
	// the default), "starttls-required", or "implicit" (TLS from the first
	// byte, e.g. port 465). Credentials are only ever sent over an encrypted
	// connection.
	TLS string
	// TLSInsecureSkipVerify disables certificate verification when set.
	TLSInsecureSkipVerify bool
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
	tlsMode := c.TLS
	if tlsMode == "" {
		tlsMode = "starttls"
	}

	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer conn.Close()

	tlsConfig := &tls.Config{
		ServerName:         c.Host,
		InsecureSkipVerify: c.TLSInsecureSkipVerify,
	}

	encrypted := false
	if tlsMode == "implicit" {
		tconn := tls.Client(conn, tlsConfig)
		if err := tconn.Handshake(); err != nil {
			return fmt.Errorf("implicit TLS handshake with %s: %w", addr, err)
		}
		conn = tconn
		encrypted = true
	}

	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return fmt.Errorf("smtp greeting from %s: %w", addr, err)
	}
	defer client.Close()

	switch tlsMode {
	case "starttls", "starttls-required":
		offered, _ := client.Extension("STARTTLS")
		if !offered {
			if tlsMode == "starttls-required" {
				return fmt.Errorf("relay %s does not offer STARTTLS (smtp.tls=starttls-required)", addr)
			}
			break // opportunistic: continue in plaintext
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS with %s: %w", addr, err)
		}
		encrypted = true
	}

	if c.Username != "" {
		if !encrypted {
			return fmt.Errorf("refusing to send SMTP credentials to %s without TLS (set smtp.tls=starttls-required or implicit, or remove smtp.username)", addr)
		}
		if err := client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return fmt.Errorf("SMTP auth with %s: %w", addr, err)
		}
	}

	if err := client.Mail(envelopeSender); err != nil {
		return fmt.Errorf("MAIL FROM to %s: %w", addr, err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("RCPT TO to %s: %w", addr, err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA to %s: %w", addr, err)
	}
	if _, err := w.Write(rawMsg); err != nil {
		w.Close()
		return fmt.Errorf("write message to %s: %w", addr, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finish message to %s: %w", addr, err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("QUIT to %s: %w", addr, err)
	}
	return nil
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
