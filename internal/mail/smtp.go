package mail

import (
	"fmt"
	"net/smtp"
)

// SMTPClient sends outbound email via an MTA's SMTP relay.
type SMTPClient struct {
	Host     string
	Port     int
	Username string
	Password string
}

// Send delivers a single message via SMTP to the MTA.
// envelopeSender is the MAIL FROM address (VERP for list posts).
// recipient is the RCPT TO address.
// rawMsg is the full RFC 822 message bytes.
func (c *SMTPClient) Send(envelopeSender, recipient string, rawMsg []byte) error {
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	var auth smtp.Auth
	if c.Username != "" {
		auth = smtp.PlainAuth("", c.Username, c.Password, c.Host)
	}
	return smtp.SendMail(addr, auth, envelopeSender, []string{recipient}, rawMsg)
}
