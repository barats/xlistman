package mail

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/mail"
	"sort"
	"strings"

	"github.com/barats/xlistman/internal/model"
)

// handleRequest processes an email sent to listname-request@domain. Each
// non-quoted line of the body is treated as a command (the Mailman -request
// convention). Results are collected and returned in a single reply to the
// sender. The sender's From address is the identity for all commands; nothing
// here is password-protected.
func (s *LMTPServer) handleRequest(ctx context.Context, p ParsedAddress, rawMsg []byte) error {
	sender, _, _ := ParseMessage(rawMsg)

	l, err := s.Store.GetList(ctx, p.ListName, p.Domain)
	if err != nil {
		return err
	}

	commands, contactMsg := parseCommands(rawMsg)
	if len(commands) == 0 {
		commands = []string{"help"}
	}

	var results []string
	for _, cmd := range commands {
		if isContactCommand(cmd) {
			results = append(results, s.cmdContact(ctx, l, sender, contactMsg))
			continue
		}
		if res := s.executeCommand(ctx, l, sender, cmd); res != "" {
			results = append(results, res)
		}
	}

	return s.enqueueCommandReply(ctx, l, sender, strings.Join(results, "\n"))
}

// parseCommands extracts command lines from a message body: one command per
// line, case-insensitive, ignoring blank lines and quoted reply text so a
// reply to a prior notice still works. A "contact" command terminates command
// parsing: everything after it (non-quoted, non-blank) is returned as the
// contact message text, since contact embeds free text rather than commands.
func parseCommands(raw []byte) (cmds []string, contactMsg string) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, ""
	}
	body, _ := io.ReadAll(msg.Body)
	var rest []string
	inContact := false
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, ">") {
			continue
		}
		if !inContact {
			cmds = append(cmds, line)
			if isContactCommand(line) {
				inContact = true
			}
			continue
		}
		rest = append(rest, line)
	}
	return cmds, strings.Join(rest, "\n")
}

// executeCommand runs a single command line and returns its reply text.
// The contact command is handled by handleRequest, which owns the rest of
// the body as the message, so it never reaches executeCommand.
func (s *LMTPServer) executeCommand(ctx context.Context, l *model.List, sender, cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	name := strings.ToLower(fields[0])
	args := fields[1:]

	switch name {
	case "help":
		return s.cmdHelp(l)
	case "which":
		return s.cmdWhich(ctx, sender)
	case "info":
		return s.cmdInfo(l)
	case "set":
		return s.cmdSet(ctx, l, sender, args)
	case "re-enable":
		return s.cmdReEnable(ctx, l, sender)
	case "unsubscribe":
		return s.cmdUnsubscribe(ctx, l, sender)
	case "who":
		return s.cmdWho(ctx, l, sender)
	default:
		return fmt.Sprintf("Unknown command %q. Reply with \"help\" for a list of commands.", name)
	}
}

// enqueueCommandReply sends the combined results back to the command sender,
// addressed from the list so it reads like a normal list message. The
// envelope sender is VERP-encoded so a failed reply is attributed as a
// bounce, not mistaken for a post.
func (s *LMTPServer) enqueueCommandReply(ctx context.Context, l *model.List, sender, body string) error {
	verpAddr, err := EncodeVERP(l.Address(), sender)
	if err != nil {
		verpAddr = l.Address()
	}
	replyTo := fmt.Sprintf("%s-request@%s", l.ListName, l.Domain)
	return s.Store.Enqueue(ctx, l.ID, l.Address(), sender,
		buildNotice(l.Address(), sender, replyTo, "Your request to "+l.Address(), body), verpAddr, "")
}

func (s *LMTPServer) cmdHelp(l *model.List) string {
	return fmt.Sprintf("Commands for %s (send one command per line):\n"+
		"  help                         show this message\n"+
		"  which                        list the lists you are subscribed to\n"+
		"  info                         show information about %s\n"+
		"  set regular|digest|nomail    change how you receive posts from %s\n"+
		"  re-enable                    reactivate your subscription to %s\n"+
		"  unsubscribe                  remove your subscription to %s\n"+
		"  who                          list the subscribers of %s (owners only)\n"+
		"  contact                      send a message to the owners of %s",
		l.Address(), l.Address(), l.Address(), l.Address(), l.Address(), l.Address(), l.Address())
}

func (s *LMTPServer) cmdWhich(ctx context.Context, sender string) string {
	sub, err := s.Store.GetSubscriber(ctx, sender)
	if err != nil {
		return "You are not subscribed to any lists."
	}
	subs, err := s.Store.ListSubscriptionsBySubscriber(ctx, sub.ID)
	if err != nil {
		return "Could not look up your subscriptions."
	}
	if len(subs) == 0 {
		return "You are not subscribed to any lists."
	}
	var lines []string
	for _, sub := range subs {
		if l, err := s.Store.GetListByID(ctx, sub.ListID); err == nil {
			lines = append(lines, fmt.Sprintf("  %s [%s]", l.Address(), sub.Status))
		}
	}
	return "You are subscribed to:\n" + strings.Join(lines, "\n")
}

func (s *LMTPServer) cmdInfo(l *model.List) string {
	return fmt.Sprintf("%s (%s list)\n%s\n\n"+
		"Subscription policy: %s\n"+
		"Moderation: %s\n"+
		"Posts: %s",
		l.Address(), l.ListType, l.Description,
		l.Settings.SubscriptionPolicy,
		yesNo(l.Settings.ModerationEnabled),
		postingRule(l))
}

func postingRule(l *model.List) string {
	if l.ListType == model.ListTypeNewsletter {
		return "only designated senders and owners may post"
	}
	if l.Settings.ModerationEnabled {
		return "all posts are held for moderator approval"
	}
	return "subscribers may post; non-subscribers are rejected"
}

func (s *LMTPServer) cmdSet(ctx context.Context, l *model.List, sender string, args []string) string {
	if len(args) != 1 {
		return "Usage: set regular|digest|nomail"
	}
	mode := model.DeliveryMode(strings.ToLower(args[0]))
	switch mode {
	case model.DeliveryModeRegular, model.DeliveryModeDigest, model.DeliveryModeNomail:
	default:
		return "Usage: set regular|digest|nomail"
	}
	sub, err := s.Store.GetSubscriber(ctx, sender)
	if err != nil {
		return "You are not subscribed to " + l.Address() + "."
	}
	subscr, err := s.Store.GetSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		return "You are not subscribed to " + l.Address() + "."
	}
	if err := s.Store.UpdateSubscriptionDelivery(ctx, subscr.ID, mode); err != nil {
		return "Could not update your delivery preference."
	}
	return fmt.Sprintf("Your delivery preference for %s is now %s.", l.Address(), mode)
}

func (s *LMTPServer) cmdReEnable(ctx context.Context, l *model.List, sender string) string {
	sub, err := s.Store.GetSubscriber(ctx, sender)
	if err != nil {
		return "You are not subscribed to " + l.Address() + "."
	}
	subscr, err := s.Store.GetSubscription(ctx, l.ID, sub.ID)
	if err != nil {
		return "You are not subscribed to " + l.Address() + "."
	}
	if subscr.Status != model.SubscriptionStatusDisabled {
		return fmt.Sprintf("Your subscription to %s is not disabled (status: %s).", l.Address(), subscr.Status)
	}
	if err := s.Store.ReenableSubscription(ctx, subscr.ID); err != nil {
		return "Could not re-enable your subscription."
	}
	return "Your subscription to " + l.Address() + " has been re-enabled."
}

func (s *LMTPServer) cmdUnsubscribe(ctx context.Context, l *model.List, sender string) string {
	sub, err := s.Store.GetSubscriber(ctx, sender)
	if err != nil {
		return "You are not subscribed to " + l.Address() + "."
	}
	if _, err := s.Store.GetSubscription(ctx, l.ID, sub.ID); err != nil {
		return "You are not subscribed to " + l.Address() + "."
	}
	if err := s.Store.DeleteSubscription(ctx, l.ID, sub.ID); err != nil {
		return "Could not remove your subscription."
	}
	return "You have been unsubscribed from " + l.Address() + "."
}

// cmdWho lists the member roster. Only owners and moderators of the list may
// view it; outsiders get a generic refusal rather than confirmation the list
// exists with members.
func (s *LMTPServer) cmdWho(ctx context.Context, l *model.List, sender string) string {
	sub, err := s.Store.GetSubscriber(ctx, sender)
	if err != nil {
		return "Only the list owners may view the subscriber list."
	}
	isOwner, _ := s.Store.IsOwner(ctx, l.ID, sub.ID)
	isModerator, _ := s.Store.IsModerator(ctx, l.ID, sub.ID)
	if !isOwner && !isModerator {
		return "Only the list owners may view the subscriber list."
	}

	subs, err := s.Store.ListSubscriptions(ctx, l.ID)
	if err != nil {
		return "Could not load the subscriber list."
	}
	seen := map[string]bool{}
	var emails []string
	for _, sub := range subs {
		subscriber, err := s.Store.GetSubscriberByID(ctx, sub.SubscriberID)
		if err != nil || seen[subscriber.Email] {
			continue
		}
		seen[subscriber.Email] = true
		emails = append(emails, subscriber.Email)
	}
	sort.Strings(emails)
	if len(emails) == 0 {
		return "This list has no subscribers."
	}
	return "Subscribers of " + l.Address() + ":\n" + strings.Join(emails, "\n")
}

func (s *LMTPServer) cmdContact(ctx context.Context, l *model.List, sender, message string) string {
	if strings.TrimSpace(message) == "" {
		return "Put your message after the contact command."
	}
	if err := s.forwardToOwners(ctx, l, sender, message); err != nil {
		return "Could not deliver your message to the owners."
	}
	return "Your message has been sent to the owners of " + l.Address() + "."
}

// handleOwnerForward forwards an email addressed to listname-owner@domain to
// all owners of the list.
func (s *LMTPServer) handleOwnerForward(ctx context.Context, p ParsedAddress, rawMsg []byte) error {
	l, err := s.Store.GetList(ctx, p.ListName, p.Domain)
	if err != nil {
		return err
	}
	sender, _, _ := ParseMessage(rawMsg)
	return s.forwardToOwners(ctx, l, sender, string(rawMsg))
}

// forwardToOwners sends message content to every owner of the list
// (deduplicated), excluding the sender. The notice sets Reply-To to the
// original sender so an owner can reply directly and reach them.
func (s *LMTPServer) forwardToOwners(ctx context.Context, l *model.List, sender, content string) error {
	owners, err := s.Store.ListOwners(ctx, l.ID)
	if err != nil {
		return err
	}
	seen := map[int64]bool{}
	var to []string
	for _, o := range owners {
		if seen[o.SubscriberID] {
			continue
		}
		seen[o.SubscriberID] = true
		owner, err := s.Store.GetSubscriberByID(ctx, o.SubscriberID)
		if err != nil {
			continue
		}
		if strings.EqualFold(owner.Email, sender) {
			continue // don't send owners their own contact
		}
		to = append(to, owner.Email)
	}
	if len(to) == 0 {
		return nil
	}

	body := fmt.Sprintf("The following message was sent to the owners of %s:\n\n%s", l.Address(), content)
	for _, addr := range to {
		if err := s.Store.Enqueue(ctx, l.ID, l.Address(), addr,
			buildNotice(l.Address(), addr, sender, "Contact to "+l.Address(), body), l.Address(), ""); err != nil {
			return err
		}
	}
	return nil
}

// isContactCommand reports whether a command line is a "contact" command.
func isContactCommand(line string) bool {
	fields := strings.Fields(line)
	return len(fields) > 0 && strings.EqualFold(fields[0], "contact")
}

func yesNo(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
