package mail

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/mail"
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
		return s.handleRequest(ctx, parsed, rawMsg)

	case AddressTypeOwner:
		return s.handleOwnerForward(ctx, parsed, rawMsg)

	case AddressTypeConfirm:
		return s.handleConfirm(ctx, parsed)

	case AddressTypeModerate:
		return s.handleModerate(ctx, parsed, rawMsg)

	default:
		return fmt.Errorf("unknown address type")
	}
}

func (s *LMTPServer) handleSubscribe(ctx context.Context, p ParsedAddress, rawMsg []byte) error {
	sender, _, _ := ParseMessage(rawMsg)
	return s.Pipeline.Subscribe(ctx, p.ListName, p.Domain, sender)
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

	// Attribute the bounce: increment the counter, auto-disable at the list's
	// threshold, and notify owners when configured (ADR 0019).
	return s.Pipeline.RecordBounce(ctx, l, subscr)
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

	// Moderated lists: tell the requester their request awaits Owner approval.
	if target == model.SubscriptionStatusHeld {
		if subscriber, err := s.Store.GetSubscriberByID(ctx, sub.SubscriberID); err == nil {
			if err := s.Pipeline.NotifySubscriptionPending(ctx, l, subscriber); err != nil {
				return err
			}
		}
	}

	return s.Store.DeleteConfirmationToken(ctx, p.EncodedPart)
}

// handleModerate processes a moderation action sent to
// listname-moderate+token@domain. The actor (the reply's sender) must be an
// Owner or Moderator of the list; the first word of the reply body selects
// the action: approve, reject, or discard.
func (s *LMTPServer) handleModerate(ctx context.Context, p ParsedAddress, rawMsg []byte) error {
	actor, _, _ := ParseMessage(rawMsg)

	held, err := s.Store.GetHeldMessageByToken(ctx, p.EncodedPart)
	if err != nil {
		return fmt.Errorf("invalid moderation token")
	}
	if time.Now().After(held.ExpiresAt) {
		return fmt.Errorf("held message has expired")
	}

	l, err := s.Store.GetListByID(ctx, held.ListID)
	if err != nil {
		return err
	}

	// Only owners and moderators of the list may act.
	sub, err := s.Store.GetSubscriber(ctx, actor)
	if err != nil {
		return fmt.Errorf("not an owner or moderator of %s", l.Address())
	}
	isOwner, _ := s.Store.IsOwner(ctx, l.ID, sub.ID)
	isModerator, _ := s.Store.IsModerator(ctx, l.ID, sub.ID)
	if !isOwner && !isModerator {
		return fmt.Errorf("not an owner or moderator of %s", l.Address())
	}

	switch moderationAction(rawMsg) {
	case "approve":
		return s.Pipeline.ApproveHeld(ctx, held.ID, model.AuditActor{Kind: model.AuditActorSubscriber, ID: sub.ID, Email: sub.Email})
	case "reject":
		return s.Pipeline.RejectHeld(ctx, held.ID, model.AuditActor{Kind: model.AuditActorSubscriber, ID: sub.ID, Email: sub.Email})
	case "discard":
		return s.Pipeline.DiscardHeld(ctx, held.ID, model.AuditActor{Kind: model.AuditActorSubscriber, ID: sub.ID, Email: sub.Email})
	default:
		return fmt.Errorf("unknown moderation action (use approve, reject, or discard)")
	}
}

// moderationAction extracts the action word from a moderation reply: the first
// word of the first non-blank, non-quoted body line.
func moderationAction(raw []byte) string {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	body, _ := io.ReadAll(msg.Body)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, ">") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		return strings.ToLower(strings.Trim(fields[0], ",;"))
	}
	return ""
}

func (c *lmtpConn) send(code int, msg string) {
	fmt.Fprintf(c.w, "%d %s\r\n", code, msg)
	c.w.Flush()
}
