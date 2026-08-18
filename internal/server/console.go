// Console handlers: the web role console (ADR 0015). Owners and Moderators act
// on a list's Held Messages using the same Pipeline actions as the email and
// CLI paths, so the three cannot drift. Owners additionally manage the
// Newsletter allowlist (Designated Senders) from here; the CLI covers server
// administrators (ADR 0005).
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	xmail "github.com/barats/xlistman/internal/mail"
	"github.com/barats/xlistman/internal/model"
)

// listHandler is a console handler that operates on an already-loaded list.
type listHandler func(w http.ResponseWriter, r *http.Request, l *model.List)

func (s *Server) registerConsoleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/console/lists", s.requireAuth(s.handleConsoleLists))
	mux.HandleFunc("/api/console/lists/", s.requireAuth(s.handleConsoleList))
}

// handleConsoleLists returns every List where the signed-in Subscriber holds
// the Owner or Moderator List Role, with the roles they hold and held counts.
func (s *Server) handleConsoleLists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sub, ok := subscriberFrom(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()

	owners, err := s.Store.ListOwnerLists(ctx, sub.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load roles"})
		return
	}
	mods, err := s.Store.ListModeratorLists(ctx, sub.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load roles"})
		return
	}

	roleMap := map[int64][]string{}
	for _, o := range owners {
		roleMap[o.ListID] = append(roleMap[o.ListID], "owner")
	}
	for _, m := range mods {
		roleMap[m.ListID] = append(roleMap[m.ListID], "moderator")
	}

	type consoleListInfo struct {
		Address   string   `json:"address"`
		ListName  string   `json:"list_name"`
		Domain    string   `json:"domain"`
		ListType  string   `json:"list_type"`
		Roles     []string `json:"roles"`
		HeldCount int      `json:"held_count"`
	}
	result := make([]consoleListInfo, 0, len(roleMap))
	for listID, roles := range roleMap {
		l, err := s.Store.GetListByID(ctx, listID)
		if err != nil {
			continue
		}
		held, err := s.Store.ListHeldMessages(ctx, listID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to load held messages"})
			return
		}
		result = append(result, consoleListInfo{
			Address:   l.Address(),
			ListName:  l.ListName,
			Domain:    l.Domain,
			ListType:  string(l.ListType),
			Roles:     roles,
			HeldCount: len(held),
		})
	}
	writeJSON(w, 200, result)
}

// handleConsoleList dispatches on the path segments after /api/console/lists/:
// {domain}/{listname}/held, /held/{id}, /held/{id}/{action}, /senders,
// /senders/{subscriberID}.
func (s *Server) handleConsoleList(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/console/lists/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	domain := parts[0]
	listName := parts[1]
	l, err := s.Store.GetList(r.Context(), listName, domain)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "list not found"})
		return
	}

	var h listHandler
	switch {
	case len(parts) == 2:
		h = s.handleConsoleListInfo
	case len(parts) >= 3 && parts[2] == "held" && len(parts) == 3:
		h = s.handleConsoleHeldList
	case len(parts) == 4 && parts[2] == "held":
		h = func(w http.ResponseWriter, r *http.Request, l *model.List) {
			s.handleConsoleHeldDetail(w, r, l, parts[3])
		}
	case len(parts) == 5 && parts[2] == "held":
		h = func(w http.ResponseWriter, r *http.Request, l *model.List) {
			s.handleConsoleHeldAction(w, r, l, parts[3], parts[4])
		}
	case len(parts) >= 3 && parts[2] == "senders" && len(parts) == 3:
		h = s.handleConsoleSenders
	case len(parts) == 4 && parts[2] == "senders":
		h = func(w http.ResponseWriter, r *http.Request, l *model.List) {
			s.handleConsoleSendersRemove(w, r, l, parts[3])
		}
	case len(parts) >= 3 && parts[2] == "settings" && len(parts) == 3:
		h = s.handleConsoleSettings
	case len(parts) >= 3 && parts[2] == "members" && len(parts) == 3:
		h = s.handleConsoleMembers
	case len(parts) == 4 && parts[2] == "members":
		h = func(w http.ResponseWriter, r *http.Request, l *model.List) {
			s.handleConsoleMemberRemove(w, r, l, parts[3])
		}
	case len(parts) == 5 && parts[2] == "members":
		h = func(w http.ResponseWriter, r *http.Request, l *model.List) {
			s.handleConsoleMemberAction(w, r, l, parts[3], parts[4])
		}
	case len(parts) == 5 && parts[2] == "roles":
		h = func(w http.ResponseWriter, r *http.Request, l *model.List) {
			s.handleConsoleRole(w, r, l, parts[3], parts[4])
		}
	case len(parts) >= 3 && parts[2] == "audit" && len(parts) == 3:
		h = s.handleConsoleAudit
	default:
		http.NotFound(w, r)
		return
	}

	// List-info and held-message routes need the Owner or Moderator role; the
	// admin sections (senders, settings, members, roles) need the Owner role.
	if len(parts) == 2 || (len(parts) >= 3 && parts[2] == "held") {
		s.requireRole(w, r, l, h)
	} else {
		s.requireOwner(w, r, l, h)
	}
}

// handleConsoleListInfo returns the list's type and the caller's roles on it,
// so the console page knows whether to show owner-only management sections.
func (s *Server) handleConsoleListInfo(w http.ResponseWriter, r *http.Request, l *model.List) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sub, ok := subscriberFrom(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	roles := []string{}
	if isOwner, _ := s.Store.IsOwner(r.Context(), l.ID, sub.ID); isOwner {
		roles = append(roles, "owner")
	}
	if isModerator, _ := s.Store.IsModerator(r.Context(), l.ID, sub.ID); isModerator {
		roles = append(roles, "moderator")
	}
	writeJSON(w, 200, map[string]any{
		"address":     l.Address(),
		"list_name":   l.ListName,
		"domain":      l.Domain,
		"list_type":   string(l.ListType),
		"description": l.Description,
		"roles":       roles,
	})
}

// requireRole gates a handler on the caller holding the Owner or Moderator
// List Role on the list, mirroring the email/CLI moderation check.
func (s *Server) requireRole(w http.ResponseWriter, r *http.Request, l *model.List, next listHandler) {
	sub, ok := subscriberFrom(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	isOwner, _ := s.Store.IsOwner(r.Context(), l.ID, sub.ID)
	isModerator, _ := s.Store.IsModerator(r.Context(), l.ID, sub.ID)
	if !isOwner && !isModerator {
		writeJSON(w, 403, map[string]string{"error": "owner or moderator required"})
		return
	}
	next(w, r, l)
}

// requireOwner gates a handler on the caller owning the list.
func (s *Server) requireOwner(w http.ResponseWriter, r *http.Request, l *model.List, next listHandler) {
	sub, ok := subscriberFrom(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	isOwner, _ := s.Store.IsOwner(r.Context(), l.ID, sub.ID)
	if !isOwner {
		writeJSON(w, 403, map[string]string{"error": "owner required"})
		return
	}
	next(w, r, l)
}

func (s *Server) handleConsoleHeldList(w http.ResponseWriter, r *http.Request, l *model.List) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	held, err := s.Store.ListHeldMessages(r.Context(), l.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load held messages"})
		return
	}
	type heldInfo struct {
		ID         int64     `json:"id"`
		Subject    string    `json:"subject"`
		Sender     string    `json:"sender"`
		ReceivedAt time.Time `json:"received_at"`
		ExpiresAt  time.Time `json:"expires_at"`
	}
	result := make([]heldInfo, 0, len(held))
	for _, m := range held {
		result = append(result, heldInfo{ID: m.ID, Subject: m.Subject, Sender: m.Sender, ReceivedAt: m.ReceivedAt, ExpiresAt: m.ExpiresAt})
	}
	writeJSON(w, 200, result)
}

func (s *Server) handleConsoleHeldDetail(w http.ResponseWriter, r *http.Request, l *model.List, idStr string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m, ok := s.heldMessageForList(w, r, l, idStr)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{
		"id":          m.ID,
		"subject":     m.Subject,
		"sender":      m.Sender,
		"body":        string(m.Body),
		"received_at": m.ReceivedAt,
		"expires_at":  m.ExpiresAt,
	})
}

// handleConsoleHeldAction runs a Moderation Action on a held message, sharing
// the Pipeline with the email and CLI paths (ADR 0015).
func (s *Server) handleConsoleHeldAction(w http.ResponseWriter, r *http.Request, l *model.List, idStr, action string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m, ok := s.heldMessageForList(w, r, l, idStr)
	if !ok {
		return
	}
	ctx := r.Context()
	actor, _ := subscriberFrom(r)
	actorRef := subscriberActor(actor)
	switch action {
	case "approve":
		if err := s.Pipeline.ApproveHeld(ctx, m.ID, actorRef); err != nil {
			s.heldActionError(w, err)
			return
		}
	case "reject":
		if err := s.Pipeline.RejectHeld(ctx, m.ID, actorRef); err != nil {
			s.heldActionError(w, err)
			return
		}
	case "discard":
		if err := s.Pipeline.DiscardHeld(ctx, m.ID, actorRef); err != nil {
			s.heldActionError(w, err)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, map[string]string{"status": action + "d"})
}

// heldMessageForList loads a held message and verifies it belongs to the list,
// writing the error response and returning ok=false on failure.
func (s *Server) heldMessageForList(w http.ResponseWriter, r *http.Request, l *model.List, idStr string) (*model.HeldMessage, bool) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	m, err := s.Store.GetHeldMessageByID(r.Context(), id)
	if err != nil || m.ListID != l.ID {
		writeJSON(w, 404, map[string]string{"error": "held message not found"})
		return nil, false
	}
	return m, true
}

func (s *Server) heldActionError(w http.ResponseWriter, err error) {
	if strings.Contains(err.Error(), "expired") {
		writeJSON(w, 409, map[string]string{"error": "held message has expired"})
		return
	}
	s.Logger.Error("moderation action", "error", err)
	writeJSON(w, 500, map[string]string{"error": "moderation action failed"})
}

// handleConsoleSenders lists a Newsletter list's Designated Senders, or adds
// one when the request is POST. Owners only (requireOwner).
func (s *Server) handleConsoleSenders(w http.ResponseWriter, r *http.Request, l *model.List) {
	ctx := r.Context()
	if r.Method == http.MethodPost {
		if l.ListType != model.ListTypeNewsletter {
			writeJSON(w, 400, map[string]string{"error": "only newsletter lists have designated senders"})
			return
		}
		var body struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request body"})
			return
		}
		email, err := normalizeEmail(body.Email)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "a valid email is required"})
			return
		}
		// Subscriber-first: only a known (verified) Subscriber can be designated.
		sub, err := s.Store.GetSubscriber(ctx, email)
		if err != nil {
			writeJSON(w, 404, map[string]string{
				"error": "unknown subscriber: " + email + ". Add them first with `xlistman subscriber add`, or have them subscribe to a list.",
			})
			return
		}
		if err := s.Store.AddDesignatedSender(ctx, l.ID, sub.ID); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to add sender"})
			return
		}
		actor, _ := subscriberFrom(r)
		s.audit(ctx, l, model.ActionSenderAdd, subscriberActor(actor), sub.Email, "")
		writeJSON(w, 201, map[string]string{"status": "sender added"})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	senders, err := s.Store.ListDesignatedSenders(ctx, l.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load senders"})
		return
	}
	type senderInfo struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}
	result := make([]senderInfo, 0, len(senders))
	for _, d := range senders {
		sub, err := s.Store.GetSubscriberByID(ctx, d.SubscriberID)
		if err != nil {
			continue
		}
		result = append(result, senderInfo{ID: sub.ID, Email: sub.Email})
	}
	writeJSON(w, 200, result)
}

func (s *Server) handleConsoleSendersRemove(w http.ResponseWriter, r *http.Request, l *model.List, subIDStr string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if l.ListType != model.ListTypeNewsletter {
		writeJSON(w, 400, map[string]string{"error": "only newsletter lists have designated senders"})
		return
	}
	subID, err := strconv.ParseInt(subIDStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.Store.RemoveDesignatedSender(r.Context(), l.ID, subID); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to remove sender"})
		return
	}
	actor, _ := subscriberFrom(r)
	s.audit(r.Context(), l, model.ActionSenderRemove, subscriberActor(actor), "", "")
	writeJSON(w, 200, map[string]string{"status": "sender removed"})
}

// handleConsoleSettings reads or writes the list's configuration (Description
// plus all ListSettings). Owners only.
func (s *Server) handleConsoleSettings(w http.ResponseWriter, r *http.Request, l *model.List) {
	ctx := r.Context()
	if r.Method == http.MethodPut {
		var body struct {
			Description string             `json:"description"`
			Settings    model.ListSettings `json:"settings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request body"})
			return
		}
		if body.Settings.ReplyToMode == model.ReplyToSpecified && strings.TrimSpace(body.Settings.ReplyToAddress) == "" {
			writeJSON(w, 400, map[string]string{"error": "a reply-to address is required when reply-to mode is 'specified'"})
			return
		}
		if body.Settings.BounceThreshold < 0 || body.Settings.HeldExpiryDays < 0 ||
			body.Settings.MaxMessageSize < 0 || body.Settings.ArchiveMaxAgeDays < 0 {
			writeJSON(w, 400, map[string]string{"error": "numeric settings cannot be negative"})
			return
		}
		if err := s.Store.UpdateListSettings(ctx, l.ID, body.Settings); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to update settings"})
			return
		}
		if err := s.Store.UpdateListDescription(ctx, l.ID, body.Description); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to update settings"})
			return
		}
		changed := l.Settings.ChangedFrom(body.Settings)
		if body.Description != l.Description {
			changed = append(changed, "description")
		}
		actor, _ := subscriberFrom(r)
		s.audit(ctx, l, model.ActionSettingsUpdate, subscriberActor(actor), l.Address(), strings.Join(changed, ", "))
		writeJSON(w, 200, map[string]string{"status": "settings updated"})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, 200, map[string]any{
		"description": l.Description,
		"list_type":   string(l.ListType),
		"settings":    l.Settings,
	})
}

// handleConsoleMembers lists the list's members (with their roles), or adds a
// member on POST. Adding is authoritative: the Owner's action replaces double
// opt-in and subscribes immediately (ADR 0016). Owners only.
func (s *Server) handleConsoleMembers(w http.ResponseWriter, r *http.Request, l *model.List) {
	ctx := r.Context()
	if r.Method == http.MethodPost {
		var body struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request body"})
			return
		}
		email, err := normalizeEmail(body.Email)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "a valid email is required"})
			return
		}
		actor, _ := subscriberFrom(r)
		if _, err := s.Pipeline.AddMember(ctx, l.ListName, l.Domain, email, subscriberActor(actor)); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 201, map[string]string{"status": "member added"})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	subs, err := s.Store.ListSubscriptions(ctx, l.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load members"})
		return
	}
	owners, _ := s.Store.ListOwners(ctx, l.ID)
	mods, _ := s.Store.ListModerators(ctx, l.ID)
	senders, _ := s.Store.ListDesignatedSenders(ctx, l.ID)

	roleBySub := map[int64][]string{}
	addRole := func(id int64, role string) {
		if roleBySub[id] == nil {
			roleBySub[id] = []string{}
		}
		roleBySub[id] = append(roleBySub[id], role)
	}
	for _, o := range owners {
		addRole(o.SubscriberID, "owner")
	}
	for _, m := range mods {
		addRole(m.SubscriberID, "moderator")
	}
	for _, d := range senders {
		addRole(d.SubscriberID, "designated_sender")
	}

	// Roles is never null so clients can rely on it being an array.
	rolesFor := func(id int64) []string {
		roles := roleBySub[id]
		if roles == nil {
			return []string{}
		}
		return roles
	}

	type memberInfo struct {
		SubscriberID   int64    `json:"subscriber_id"`
		Email          string   `json:"email"`
		SubscriptionID *int64   `json:"subscription_id,omitempty"`
		Status         *string  `json:"status,omitempty"`
		DeliveryMode   *string  `json:"delivery_mode,omitempty"`
		BounceCount    int      `json:"bounce_count"`
		Roles          []string `json:"roles"`
	}
	result := make([]memberInfo, 0, len(subs))
	seen := map[int64]bool{}
	for _, sub := range subs {
		subscriber, err := s.Store.GetSubscriberByID(ctx, sub.SubscriberID)
		if err != nil {
			continue
		}
		subID := sub.ID
		status := string(sub.Status)
		mode := string(sub.DeliveryMode)
		seen[sub.SubscriberID] = true
		result = append(result, memberInfo{
			SubscriberID:   subscriber.ID,
			Email:          subscriber.Email,
			SubscriptionID: &subID,
			Status:         &status,
			DeliveryMode:   &mode,
			BounceCount:    sub.BounceCount,
			Roles:          rolesFor(sub.SubscriberID),
		})
	}
	// Role holders who are not subscribed still appear so roles can be managed.
	for id := range roleBySub {
		if seen[id] {
			continue
		}
		subscriber, err := s.Store.GetSubscriberByID(ctx, id)
		if err != nil {
			continue
		}
		result = append(result, memberInfo{SubscriberID: subscriber.ID, Email: subscriber.Email, Roles: rolesFor(id)})
	}
	writeJSON(w, 200, result)
}

// handleConsoleMemberRemove removes a member (DELETE /members/{subscriberID}).
func (s *Server) handleConsoleMemberRemove(w http.ResponseWriter, r *http.Request, l *model.List, subIDStr string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	subID, err := strconv.ParseInt(subIDStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	actor, _ := subscriberFrom(r)
	if err := s.Pipeline.RemoveMember(r.Context(), l.ID, subID, subscriberActor(actor)); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "member removed"})
}

// handleConsoleMemberAction approves or rejects a Held Subscription
// (POST /members/{subscriberID}/{approve|reject}).
func (s *Server) handleConsoleMemberAction(w http.ResponseWriter, r *http.Request, l *model.List, subIDStr, action string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	subID, err := strconv.ParseInt(subIDStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	actor, _ := subscriberFrom(r)
	actorRef := subscriberActor(actor)
	switch action {
	case "approve":
		err = s.Pipeline.ApproveSubscription(ctx, l.ID, subID, actorRef)
	case "reject":
		err = s.Pipeline.RejectSubscription(ctx, l.ID, subID, actorRef)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "subscription " + action + "d"})
}

// handleConsoleRole grants (POST) or revokes (DELETE) the Owner or Moderator
// List Role for a Subscriber. Revoking enforces the last-owner guard.
func (s *Server) handleConsoleRole(w http.ResponseWriter, r *http.Request, l *model.List, subIDStr, role string) {
	if role != xmail.RoleOwner && role != xmail.RoleModerator {
		http.NotFound(w, r)
		return
	}
	subID, err := strconv.ParseInt(subIDStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	actor, _ := subscriberFrom(r)
	actorRef := subscriberActor(actor)
	switch r.Method {
	case http.MethodPost:
		err = s.Pipeline.GrantRole(ctx, l.ID, subID, role, actorRef)
	case http.MethodDelete:
		err = s.Pipeline.RevokeRole(ctx, l.ID, subID, role, actorRef)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "last owner") {
			writeJSON(w, 409, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, 200, map[string]string{"status": "role updated"})
}
