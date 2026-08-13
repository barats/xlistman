package mail

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/barat/xlistman/internal/store"
)

// LMTPServer receives inbound mail from the MTA via LMTP (RFC 2033).
type LMTPServer struct {
	Addr    string
	Store   store.Store
	Pipeline *Pipeline
}

// ListenAndServe starts the LMTP server.
func (s *LMTPServer) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("lmtp listen: %w", err)
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("lmtp accept: %w", err)
			}
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *LMTPServer) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	c := &lmtpConn{
		server: s,
		conn:   conn,
		r:      bufio.NewReader(conn),
		w:      bufio.NewWriter(conn),
	}
	c.serve(ctx)
}

type lmtpConn struct {
	server *LMTPServer
	conn   net.Conn
	r      *bufio.Reader
	w      *bufio.Writer

	mailFrom  string
	rcptTos   []string
}

func (c *lmtpConn) serve(ctx context.Context) {
	c.send(220, "xListman LMTP ready")

	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		parts := strings.SplitN(line, " ", 2)
		cmd := strings.ToUpper(parts[0])
		arg := ""
		if len(parts) > 1 {
			arg = parts[1]
		}

		switch cmd {
		case "LHLO":
			c.send(250, "xListman")
		case "MAIL":
			c.handleMail(arg)
		case "RCPT":
			c.handleRcpt(arg)
		case "DATA":
			c.handleData(ctx)
		case "RSET":
			c.mailFrom = ""
			c.rcptTos = nil
			c.send(250, "OK")
		case "NOOP":
			c.send(250, "OK")
		case "QUIT":
			c.send(221, "Bye")
			return
		default:
			c.send(500, "Unknown command")
		}
	}
}

func (c *lmtpConn) handleMail(arg string) {
	// Parse "FROM:<addr>".
	if !strings.HasPrefix(strings.ToUpper(arg), "FROM:") {
		c.send(501, "Syntax error")
		return
	}
	addr := strings.TrimSpace(arg[5:])
	addr = strings.Trim(addr, "<>")
	c.mailFrom = addr
	c.rcptTos = nil
	c.send(250, "OK")
}

func (c *lmtpConn) handleRcpt(arg string) {
	if c.mailFrom == "" {
		c.send(503, "MAIL first")
		return
	}
	if !strings.HasPrefix(strings.ToUpper(arg), "TO:") {
		c.send(501, "Syntax error")
		return
	}
	addr := strings.TrimSpace(arg[3:])
	addr = strings.Trim(addr, "<>")
	c.rcptTos = append(c.rcptTos, addr)
	c.send(250, "OK")
}

func (c *lmtpConn) handleData(ctx context.Context) {
	if len(c.rcptTos) == 0 {
		c.send(503, "RCPT first")
		return
	}
	c.send(354, "Start mail input")

	// Read message until lone dot.
	var msgBuilder strings.Builder
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return
		}
		stripped := strings.TrimRight(line, "\r\n")
		if stripped == "." {
			break
		}
		// Unstuff leading dots.
		if strings.HasPrefix(stripped, "..") {
			stripped = stripped[1:]
		}
		msgBuilder.WriteString(stripped)
		msgBuilder.WriteString("\r\n")
	}

	rawMsg := []byte(msgBuilder.String())

	// Process each recipient and return per-recipient status.
	for _, rcpt := range c.rcptTos {
		err := c.processRecipient(ctx, rcpt, rawMsg)
		if err != nil {
			c.send(550, fmt.Sprintf("Failed: %v", err))
		} else {
			c.send(250, "OK")
		}
	}

	c.mailFrom = ""
	c.rcptTos = nil
}

func (c *lmtpConn) processRecipient(ctx context.Context, rcpt string, rawMsg []byte) error {
	parsed, err := ParseAddress(rcpt)
	if err != nil {
		return err
	}

	switch parsed.Type {
	case AddressTypePost:
		sender, _, _ := ParseMessage(rawMsg)
		return c.server.Pipeline.ProcessPost(ctx, parsed.ListName, parsed.Domain, sender, rawMsg)

	case AddressTypeSubscribe:
		return c.handleSubscribe(ctx, parsed, rawMsg)

	case AddressTypeUnsubscribe:
		return c.handleUnsubscribe(ctx, parsed, rawMsg)

	case AddressTypeBounce:
		return c.handleBounce(ctx, parsed)

	case AddressTypeRequest:
		// TODO: parse email commands from body
		return nil

	case AddressTypeOwner:
		// Forward to list owners
		return nil

	case AddressTypeConfirm:
		return c.handleConfirm(ctx, parsed)

	default:
		return fmt.Errorf("unknown address type")
	}
}

func (c *lmtpConn) handleSubscribe(ctx context.Context, p ParsedAddress, rawMsg []byte) error {
	sender, _, _ := ParseMessage(rawMsg)
	sub, err := c.server.Store.GetOrCreateSubscriber(ctx, sender)
	if err != nil {
		return err
	}
	l, err := c.server.Store.GetList(ctx, p.ListName, p.Domain)
	if err != nil {
		return err
	}

	// Check subscription policy.
	switch l.Settings.SubscriptionPolicy {
	case "closed":
		return fmt.Errorf("list is closed for subscriptions")
	}

	// Create a pending subscription.
	subscr, err := c.server.Store.CreateSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		return err // likely already subscribed
	}

	// Create confirmation token (double opt-in).
	token, err := c.server.Store.CreateConfirmationToken(ctx, l.ID, sub.ID, sender, time.Now().Add(48*time.Hour))
	if err != nil {
		return err
	}

	_ = subscr
	_ = token
	// TODO: Enqueue confirmation email with the token.
	return nil
}

func (c *lmtpConn) handleUnsubscribe(ctx context.Context, p ParsedAddress, rawMsg []byte) error {
	sender, _, _ := ParseMessage(rawMsg)
	sub, err := c.server.Store.GetSubscriber(ctx, sender)
	if err != nil {
		return nil // not subscribed, nothing to do
	}
	l, err := c.server.Store.GetList(ctx, p.ListName, p.Domain)
	if err != nil {
		return err
	}
	return c.server.Store.DeleteSubscription(ctx, l.ID, sub.ID)
}

func (c *lmtpConn) handleBounce(ctx context.Context, p ParsedAddress) error {
	// Decode the VERP address to find the recipient.
	_, recipientAddr, err := DecodeVERP(p.ListName + "-bounces+" + p.EncodedPart + "@" + p.Domain)
	if err != nil {
		return err
	}

	sub, err := c.server.Store.GetSubscriber(ctx, recipientAddr)
	if err != nil {
		return nil
	}

	// Find the subscription for this list.
	l, err := c.server.Store.GetList(ctx, p.ListName, p.Domain)
	if err != nil {
		return err
	}

	subscr, err := c.server.Store.GetSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		return nil
	}

	// Increment bounce count and auto-disable if threshold exceeded.
	if err := c.server.Store.IncrementBounceCount(ctx, subscr.ID); err != nil {
		return err
	}

	updated, _ := c.server.Store.GetSubscription(ctx, l.ID, sub.ID)
	if updated.BounceCount >= l.Settings.BounceThreshold {
		c.server.Store.DisableSubscription(ctx, subscr.ID)
	}

	return nil
}

func (c *lmtpConn) handleConfirm(ctx context.Context, p ParsedAddress) error {
	ct, err := c.server.Store.GetConfirmationToken(ctx, p.EncodedPart)
	if err != nil {
		return fmt.Errorf("invalid or expired token")
	}

	// Activate the subscription.
	_, err = c.server.Store.GetSubscription(ctx, ct.ListID, ct.SubscriberID)
	if err != nil {
		return err
	}

	// TODO: mark subscription as confirmed via store.

	c.server.Store.DeleteConfirmationToken(ctx, p.EncodedPart)
	return nil
}

func (c *lmtpConn) send(code int, msg string) {
	fmt.Fprintf(c.w, "%d %s\r\n", code, msg)
	c.w.Flush()
}
