package mail

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store"
)

// LMTPServer receives inbound mail from the MTA via LMTP (RFC 2033).
type LMTPServer struct {
	Addr     string
	Store    store.Store
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

	mailFrom string
	rcptTos  []string
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
		err := c.server.processRecipient(ctx, rcpt, rawMsg)
		if err != nil {
			c.send(550, fmt.Sprintf("Failed: %v", err))
		} else {
			c.send(250, "OK")
		}
	}

	c.mailFrom = ""
	c.rcptTos = nil
}

// ProcessMessage routes a single message to one recipient. It is the shared
// entry point for both the LMTP inbound path and the pipe-mode Unix socket.
func (s *LMTPServer) ProcessMessage(ctx context.Context, rcpt string, rawMsg []byte) error {
	return s.processRecipient(ctx, rcpt, rawMsg)
}

func (s *LMTPServer) processRecipient(ctx context.Context, rcpt string, rawMsg []byte) error {
	parsed, err := ParseAddress(rcpt)
	if err != nil {
		return err
	}

	switch parsed.Type {
	case AddressTypePost:
		sender, _, _ := ParseMessage(rawMsg)
		return s.Pipeline.ProcessPost(ctx, parsed.ListName, parsed.Domain, sender, rawMsg)

	case AddressTypeSubscribe:
		return s.handleSubscribe(ctx, parsed, rawMsg)

	case AddressTypeUnsubscribe:
		return s.handleUnsubscribe(ctx, parsed, rawMsg)

	case AddressTypeBounce:
		return s.handleBounce(ctx, parsed)

	case AddressTypeRequest:
		// TODO: parse email commands from body
		return nil

	case AddressTypeOwner:
		// Forward to list owners
		return nil

	case AddressTypeConfirm:
		return s.handleConfirm(ctx, parsed)

	default:
		return fmt.Errorf("unknown address type")
	}
}

func (s *LMTPServer) handleSubscribe(ctx context.Context, p ParsedAddress, rawMsg []byte) error {
	sender, _, _ := ParseMessage(rawMsg)
	sub, err := s.Store.GetOrCreateSubscriber(ctx, sender)
	if err != nil {
		return err
	}
	l, err := s.Store.GetList(ctx, p.ListName, p.Domain)
	if err != nil {
		return err
	}

	// Closed lists don't accept self-service subscriptions.
	if l.Settings.SubscriptionPolicy == model.SubscriptionPolicyClosed {
		return fmt.Errorf("list is closed for subscriptions")
	}

	// A repeat request for a pending subscription re-sends the confirmation
	// email instead of erroring, so a lost email can't permanently block a join.
	if existing, err := s.Store.GetSubscription(ctx, l.ID, sub.ID); err == nil {
		switch existing.Status {
		case model.SubscriptionStatusPending:
			token, err := s.Store.CreateConfirmationToken(ctx, l.ID, sub.ID, sender, time.Now().Add(48*time.Hour))
			if err != nil {
				return err
			}
			return s.enqueueConfirmation(ctx, l, sub, token)
		case model.SubscriptionStatusActive, model.SubscriptionStatusHeld:
			return fmt.Errorf("already subscribed")
		case model.SubscriptionStatusDisabled:
			return fmt.Errorf("already subscribed but disabled; re-enabling is not yet supported")
		}
	}

	// Create a pending subscription (double opt-in).
	if _, err := s.Store.CreateSubscription(ctx, l.ID, sub.ID); err != nil {
		return err // likely already subscribed
	}

	token, err := s.Store.CreateConfirmationToken(ctx, l.ID, sub.ID, sender, time.Now().Add(48*time.Hour))
	if err != nil {
		return err
	}

	return s.enqueueConfirmation(ctx, l, sub, token)
}

// enqueueConfirmation builds and enqueues the double opt-in confirmation email.
// The subscriber confirms by replying to the confirmation address, so the
// message sets Reply-To to that address and the body instructs a reply.
func (s *LMTPServer) enqueueConfirmation(ctx context.Context, l *model.List, sub *model.Subscriber, token string) error {
	confirmAddr := fmt.Sprintf("%s-confirm+%s@%s", l.ListName, token, l.Domain)
	date := time.Now().UTC().Format(time.RFC1123Z)
	raw := fmt.Sprintf("From: %s\r\nTo: %s\r\nReply-To: %s\r\nSubject: Confirm your subscription to %s\r\nDate: %s\r\n\r\n"+
		"Reply to this message to confirm your subscription to %s.\r\n\r\n"+
		"If you did not request this subscription, you can safely ignore this message.\r\n",
		l.Address(), sub.Email, confirmAddr, l.Address(), date, l.Address())
	return s.Store.Enqueue(ctx, l.ID, l.Address(), sub.Email, []byte(raw), l.Address())
}

func (s *LMTPServer) handleUnsubscribe(ctx context.Context, p ParsedAddress, rawMsg []byte) error {
	sender, _, _ := ParseMessage(rawMsg)
	sub, err := s.Store.GetSubscriber(ctx, sender)
	if err != nil {
		return nil // not subscribed, nothing to do
	}
	l, err := s.Store.GetList(ctx, p.ListName, p.Domain)
	if err != nil {
		return err
	}
	return s.Store.DeleteSubscription(ctx, l.ID, sub.ID)
}

func (s *LMTPServer) handleBounce(ctx context.Context, p ParsedAddress) error {
	// Decode the VERP address to find the recipient.
	_, recipientAddr, err := DecodeVERP(p.ListName + "-bounces+" + p.EncodedPart + "@" + p.Domain)
	if err != nil {
		return err
	}

	sub, err := s.Store.GetSubscriber(ctx, recipientAddr)
	if err != nil {
		return nil
	}

	// Find the subscription for this list.
	l, err := s.Store.GetList(ctx, p.ListName, p.Domain)
	if err != nil {
		return err
	}

	subscr, err := s.Store.GetSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		return nil
	}

	// Increment bounce count and auto-disable if threshold exceeded.
	if err := s.Store.IncrementBounceCount(ctx, subscr.ID); err != nil {
		return err
	}

	updated, _ := s.Store.GetSubscription(ctx, l.ID, sub.ID)
	if updated.BounceCount >= l.Settings.BounceThreshold {
		s.Store.SetSubscriptionStatus(ctx, subscr.ID, model.SubscriptionStatusDisabled)
	}

	return nil
}

func (s *LMTPServer) handleConfirm(ctx context.Context, p ParsedAddress) error {
	ct, err := s.Store.GetConfirmationToken(ctx, p.EncodedPart)
	if err != nil {
		return fmt.Errorf("invalid or expired token")
	}

	sub, err := s.Store.GetSubscription(ctx, ct.ListID, ct.SubscriberID)
	if err != nil {
		return err
	}

	l, err := s.Store.GetListByID(ctx, ct.ListID)
	if err != nil {
		return err
	}

	// Double opt-in complete: Open lists activate immediately, Moderated lists
	// hold the subscription for owner approval.
	target := model.SubscriptionStatusActive
	if l.Settings.SubscriptionPolicy == model.SubscriptionPolicyModerated {
		target = model.SubscriptionStatusHeld
	}
	if err := s.Store.ConfirmSubscription(ctx, sub.ID, target); err != nil {
		return err
	}

	return s.Store.DeleteConfirmationToken(ctx, p.EncodedPart)
}

func (c *lmtpConn) send(code int, msg string) {
	fmt.Fprintf(c.w, "%d %s\r\n", code, msg)
	c.w.Flush()
}
