// Package server implements the xListman HTTP server (API + health + metrics).
//
// The web UI is a SvelteKit SPA (ADR 0007); this server exposes the JSON API
// it consumes. Authentication is passwordless: one-time Magic Links (ADR 0003,
// 0012) establish a Session carried in a server-side cookie.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	netmail "net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/barats/xlistman/internal/config"
	xmail "github.com/barats/xlistman/internal/mail"
	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store"
)

const (
	sessionCookieName = "xlistman_session"
	magicLinkTTL      = 30 * time.Minute
	sessionTTL        = 30 * 24 * time.Hour
)

type contextKey string

const contextKeySubscriber contextKey = "subscriber"

// Server is the HTTP server for xListman.
type Server struct {
	Store     store.Store
	Config    *config.Config
	Logger    *slog.Logger
	Pipeline  *xmail.Pipeline
	cookieSec bool
	handler   http.Handler
	httpSrv   *http.Server
}

// New creates a new HTTP server. webFS optionally carries the embedded
// SvelteKit SPA build (web/build); when present it is served with an
// index.html fallback for client-side routes (ADR 0007).
func New(cfg *config.Config, st store.Store, logger *slog.Logger, pipeline *xmail.Pipeline, webFS ...fs.FS) *Server {
	s := &Server{
		Store:     st,
		Config:    cfg,
		Logger:    logger,
		Pipeline:  pipeline,
		cookieSec: strings.HasPrefix(cfg.Web.BaseURL, "https://"),
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.handler = s.composeHandler(mux, webFS)
	s.httpSrv = &http.Server{
		Addr:         cfg.HTTP.Listen,
		Handler:      s.handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	return s
}

// composeHandler routes API and health requests to the mux and everything
// else to the embedded SPA.
func (s *Server) composeHandler(mux *http.ServeMux, webFS []fs.FS) http.Handler {
	spa := s.spaHandler(webFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
			mux.ServeHTTP(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	})
}

// spaHandler serves the embedded SPA static files, falling back to index.html
// for client-side routes. When no build is embedded, it returns 404.
func (s *Server) spaHandler(webFS []fs.FS) http.Handler {
	if len(webFS) == 0 || webFS[0] == nil {
		return http.NotFoundHandler()
	}
	sub, err := fs.Sub(webFS[0], "web/build")
	if err != nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(sub, "index.html"); err == nil {
			clone := r.Clone(r.Context())
			clone.URL.Path = "/"
			fileServer.ServeHTTP(w, clone)
			return
		}
		http.NotFound(w, r)
	})
}

// Handler returns the HTTP handler (the registered routes), for tests and for
// serving behind the SPA static handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	return s.httpSrv.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/lists", s.handleLists)
	mux.HandleFunc("/api/lists/", s.handleListDetail)
	mux.HandleFunc("/api/auth/magic-link", s.handleMagicLink)
	mux.HandleFunc("/api/auth/verify", s.handleVerify)
	mux.HandleFunc("/api/auth/logout", s.handleLogout)
	mux.HandleFunc("/api/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("/api/me/held-posts", s.requireAuth(s.handleMyHeldPosts))
	mux.HandleFunc("/api/me/subscriptions/", s.requireAuth(s.handleMySubscription))
	s.registerConsoleRoutes(mux)
	s.registerAdminRoutes(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	depth, _ := s.Store.QueueDepth(r.Context())
	writeJSON(w, 200, map[string]any{
		"status":      "ok",
		"queue_depth": depth,
	})
}

func (s *Server) handleLists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	domain := r.URL.Query().Get("domain")
	lists, err := s.Store.ListLists(r.Context(), domain)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to list lists"})
		return
	}
	type listInfo struct {
		Address     string `json:"address"`
		ListName    string `json:"list_name"`
		Domain      string `json:"domain"`
		Description string `json:"description"`
		ListType    string `json:"list_type"`
	}
	result := make([]listInfo, len(lists))
	for i, l := range lists {
		result[i] = listInfo{
			Address:     l.Address(),
			ListName:    l.ListName,
			Domain:      l.Domain,
			Description: l.Description,
			ListType:    string(l.ListType),
		}
	}
	writeJSON(w, 200, result)
}

// handleListDetail dispatches on the path segments after /api/lists/:
// {domain}/{listname}, {domain}/{listname}/subscribe,
// {domain}/{listname}/archives, {domain}/{listname}/archives/{id}.
func (s *Server) handleListDetail(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/lists/"), "/")
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

	switch {
	case len(parts) >= 3 && parts[2] == "subscribe":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleSubscribe(w, r, l)
	case len(parts) >= 3 && parts[2] == "archives" && len(parts) == 3:
		s.handleArchives(w, r, l)
	case len(parts) >= 4 && parts[2] == "archives":
		s.handleArchiveEntry(w, r, l, parts[3])
	default:
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleListInfo(w, r, l)
	}
}

func (s *Server) handleListInfo(w http.ResponseWriter, r *http.Request, l *model.List) {
	writeJSON(w, 200, map[string]any{
		"address":             l.Address(),
		"list_name":           l.ListName,
		"domain":              l.Domain,
		"description":         l.Description,
		"list_type":           string(l.ListType),
		"subscription_policy": string(l.Settings.SubscriptionPolicy),
		"moderation_enabled":  l.Settings.ModerationEnabled,
		"digest_frequency":    string(l.Settings.DigestFrequency),
	})
}

// handleSubscribe is the web entry point for the double opt-in flow. It shares
// Pipeline.Subscribe with the email path, so both act identically.
func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request, l *model.List) {
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

	if err := s.Pipeline.Subscribe(r.Context(), l.ListName, l.Domain, email); err != nil {
		switch {
		case strings.Contains(err.Error(), "closed for subscriptions"):
			writeJSON(w, 403, map[string]string{"error": err.Error()})
		case strings.Contains(err.Error(), "already subscribed"):
			writeJSON(w, 409, map[string]string{"error": err.Error()})
		default:
			s.Logger.Error("subscribe", "list", l.Address(), "email", email, "error", err)
			writeJSON(w, 500, map[string]string{"error": "failed to subscribe"})
		}
		return
	}
	writeJSON(w, 202, map[string]string{"status": "confirmation email sent"})
}

// handleMagicLink emails a one-time login link to an existing subscriber.
// The response is always 202 so the endpoint cannot be used to enumerate
// known addresses.
func (s *Server) handleMagicLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

	// Only send a link to a known subscriber; unknown addresses still get 202.
	sub, err := s.Store.GetSubscriber(r.Context(), email)
	if err != nil {
		writeJSON(w, 202, map[string]string{"status": "magic link sent"})
		return
	}

	token, err := s.Store.CreateMagicLink(r.Context(), sub.ID, email, time.Now().Add(magicLinkTTL))
	if err != nil {
		s.Logger.Error("create magic link", "email", email, "error", err)
		writeJSON(w, 500, map[string]string{"error": "failed to send magic link"})
		return
	}

	link := s.Config.Web.BaseURL + "/api/auth/verify?token=" + token
	msg := buildTextEmail("xListman", email, "Your xListman login link",
		"Use this link to sign in to your subscriptions. It expires in 30 minutes.\n\n"+link+"\n\nIf you did not request this, you can ignore this message.")
	if err := s.Store.Enqueue(r.Context(), 0, "", email, msg, "", ""); err != nil {
		s.Logger.Error("enqueue magic link", "email", email, "error", err)
		writeJSON(w, 500, map[string]string{"error": "failed to send magic link"})
		return
	}
	writeJSON(w, 202, map[string]string{"status": "magic link sent"})
}

// handleVerify consumes a magic link token, creates a Session, sets the
// session cookie, and redirects into the SPA.
func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	base := s.Config.Web.BaseURL
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Redirect(w, r, base+"/auth?error=invalid", http.StatusFound)
		return
	}

	ml, err := s.Store.GetMagicLink(r.Context(), token)
	if err != nil || time.Now().After(ml.ExpiresAt) {
		if err == nil {
			s.Store.DeleteMagicLink(r.Context(), token)
		}
		http.Redirect(w, r, base+"/auth?error=invalid", http.StatusFound)
		return
	}

	// One-time use: consume the token, then issue a session.
	s.Store.DeleteMagicLink(r.Context(), token)
	sessionID, err := s.Store.CreateSession(r.Context(), ml.SubscriberID, ml.Email, time.Now().Add(sessionTTL))
	if err != nil {
		s.Logger.Error("create session", "email", ml.Email, "error", err)
		writeJSON(w, 500, map[string]string{"error": "failed to start session"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSec,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	http.Redirect(w, r, base+"/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		s.Store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSec,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, 200, map[string]string{"status": "logged out"})
}

// handleMe returns the authenticated subscriber and their subscriptions.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sub, ok := subscriberFrom(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	subs, err := s.Store.ListSubscriptionsBySubscriber(r.Context(), sub.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load subscriptions"})
		return
	}
	type subInfo struct {
		ID           int64  `json:"id"`
		Address      string `json:"address"`
		ListName     string `json:"list_name"`
		Domain       string `json:"domain"`
		ListType     string `json:"list_type"`
		Status       string `json:"status"`
		DeliveryMode string `json:"delivery_mode"`
		BounceCount  int    `json:"bounce_count"`
	}
	infos := make([]subInfo, 0, len(subs))
	for _, sub := range subs {
		l, err := s.Store.GetListByID(r.Context(), sub.ListID)
		if err != nil {
			continue
		}
		infos = append(infos, subInfo{
			ID:           sub.ID,
			Address:      l.Address(),
			ListName:     l.ListName,
			Domain:       l.Domain,
			ListType:     string(l.ListType),
			Status:       string(sub.Status),
			DeliveryMode: string(sub.DeliveryMode),
			BounceCount:  sub.BounceCount,
		})
	}
	isAdmin, _ := s.Store.IsAdministrator(r.Context(), sub.ID)
	owners, _ := s.Store.ListOwnerLists(r.Context(), sub.ID)
	mods, _ := s.Store.ListModeratorLists(r.Context(), sub.ID)
	hasListRole := len(owners) > 0 || len(mods) > 0
	writeJSON(w, 200, map[string]any{
		"email":            sub.Email,
		"subscriptions":    infos,
		"is_administrator": isAdmin,
		"has_list_role":    hasListRole,
	})
}

// handleMyHeldPosts returns the authenticated subscriber's own posts currently
// awaiting moderation approval, across all lists (the sender held-status
// view). Read-only; moderation itself stays with Owners and Moderators.
func (s *Server) handleMyHeldPosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sub, ok := subscriberFrom(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	held, err := s.Store.ListHeldMessagesBySender(r.Context(), sub.Email)
	if err != nil {
		s.Logger.Error("list held posts", "email", sub.Email, "error", err)
		writeJSON(w, 500, map[string]string{"error": "failed to load held posts"})
		return
	}
	type heldPostInfo struct {
		ID         int64  `json:"id"`
		ListAddr   string `json:"list_addr"`
		Subject    string `json:"subject"`
		ReceivedAt string `json:"received_at"`
		ExpiresAt  string `json:"expires_at"`
	}
	result := make([]heldPostInfo, 0, len(held))
	for _, m := range held {
		l, err := s.Store.GetListByID(r.Context(), m.ListID)
		if err != nil {
			continue
		}
		result = append(result, heldPostInfo{
			ID:         m.ID,
			ListAddr:   l.Address(),
			Subject:    m.Subject,
			ReceivedAt: m.ReceivedAt.UTC().Format("2006-01-02T15:04:05Z"),
			ExpiresAt:  m.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	writeJSON(w, 200, result)
}

// handleMySubscription dispatches self-service actions on one of the caller's
// own subscriptions: delivery, re-enable, unsubscribe.
func (s *Server) handleMySubscription(w http.ResponseWriter, r *http.Request) {
	sub, ok := subscriberFrom(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/me/subscriptions/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	subscr, err := s.Store.GetSubscriptionByID(r.Context(), id)
	if err != nil || subscr.SubscriberID != sub.ID {
		writeJSON(w, 404, map[string]string{"error": "subscription not found"})
		return
	}

	switch parts[1] {
	case "delivery":
		s.handleSetDelivery(w, r, subscr)
	case "re-enable":
		s.handleReEnable(w, r, subscr)
	case "unsubscribe":
		s.handleUnsubscribeMe(w, r, subscr)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleSetDelivery(w http.ResponseWriter, r *http.Request, subscr *model.Subscription) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	switch model.DeliveryMode(body.Mode) {
	case model.DeliveryModeRegular, model.DeliveryModeDigest, model.DeliveryModeNomail:
	default:
		writeJSON(w, 400, map[string]string{"error": "mode must be regular, digest, or nomail"})
		return
	}
	if err := s.Store.UpdateSubscriptionDelivery(r.Context(), subscr.ID, model.DeliveryMode(body.Mode)); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to update delivery"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "delivery updated"})
}

func (s *Server) handleReEnable(w http.ResponseWriter, r *http.Request, subscr *model.Subscription) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if subscr.Status != model.SubscriptionStatusDisabled {
		writeJSON(w, 409, map[string]string{"error": "subscription is not disabled"})
		return
	}
	if err := s.Store.ReenableSubscription(r.Context(), subscr.ID); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to re-enable subscription"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "subscription re-enabled"})
}

func (s *Server) handleUnsubscribeMe(w http.ResponseWriter, r *http.Request, subscr *model.Subscription) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.Store.DeleteSubscription(r.Context(), subscr.ListID, subscr.SubscriberID); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to unsubscribe"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "unsubscribed"})
}

// handleArchives lists a list's archive (optionally full-text searched),
// members-only per the Archive glossary term.
func (s *Server) handleArchives(w http.ResponseWriter, r *http.Request, l *model.List) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireMember(w, r, l) {
		return
	}

	limit := queryIntDefault(r, "limit", 50)
	if limit > 200 {
		limit = 200
	}
	offset := queryIntDefault(r, "offset", 0)
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	var (
		entries []model.ArchiveEntry
		err     error
	)
	if q != "" {
		entries, err = s.Store.SearchArchive(r.Context(), l.ID, q, limit)
	} else {
		entries, err = s.Store.ListArchive(r.Context(), l.ID, limit, offset)
	}
	if err != nil {
		s.Logger.Error("list archive", "list", l.Address(), "error", err)
		writeJSON(w, 500, map[string]string{"error": "failed to load archive"})
		return
	}

	type entryInfo struct {
		ID         int64     `json:"id"`
		Subject    string    `json:"subject"`
		From       string    `json:"from"`
		MessageID  string    `json:"message_id"`
		ThreadID   string    `json:"thread_id"`
		ReceivedAt time.Time `json:"received_at"`
	}
	result := make([]entryInfo, 0, len(entries))
	for _, e := range entries {
		result = append(result, entryInfo{
			ID:         e.ID,
			Subject:    e.Subject,
			From:       e.From,
			MessageID:  e.MessageID,
			ThreadID:   e.ThreadID,
			ReceivedAt: e.ReceivedAt,
		})
	}
	writeJSON(w, 200, result)
}

// handleArchiveEntry returns one archived message with its body.
func (s *Server) handleArchiveEntry(w http.ResponseWriter, r *http.Request, l *model.List, idStr string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireMember(w, r, l) {
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	e, err := s.Store.GetArchiveEntry(r.Context(), id)
	if err != nil || e.ListID != l.ID {
		writeJSON(w, 404, map[string]string{"error": "message not found"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"id":          e.ID,
		"subject":     e.Subject,
		"from":        e.From,
		"message_id":  e.MessageID,
		"thread_id":   e.ThreadID,
		"received_at": e.ReceivedAt,
		"body":        string(e.Body),
	})
}

// requireAuth wraps a handler so it only runs for an authenticated subscriber.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sub, ok := s.authenticate(r)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := context.WithValue(r.Context(), contextKeySubscriber, sub)
		next(w, r.WithContext(ctx))
	}
}

// requireMember checks the caller is a Member (Active or Held subscription)
// of the list, per the members-only Archive rule. On failure it writes the
// response and returns false.
func (s *Server) requireMember(w http.ResponseWriter, r *http.Request, l *model.List) bool {
	sub, ok := s.authenticate(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return false
	}
	subscr, err := s.Store.GetSubscription(r.Context(), l.ID, sub.ID)
	if err != nil || (subscr.Status != model.SubscriptionStatusActive && subscr.Status != model.SubscriptionStatusHeld) {
		writeJSON(w, 403, map[string]string{"error": "members only"})
		return false
	}
	return true
}

// authenticate resolves the session cookie to a Subscriber, or returns ok=false.
func (s *Server) authenticate(r *http.Request) (*model.Subscriber, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil, false
	}
	sess, err := s.Store.GetSession(r.Context(), c.Value)
	if err != nil {
		return nil, false
	}
	sub, err := s.Store.GetSubscriberByID(r.Context(), sess.SubscriberID)
	if err != nil {
		return nil, false
	}
	return sub, true
}

func subscriberFrom(r *http.Request) (*model.Subscriber, bool) {
	sub, ok := r.Context().Value(contextKeySubscriber).(*model.Subscriber)
	return sub, ok
}

// normalizeEmail parses and lowercases an email address for the web layer.
func normalizeEmail(raw string) (string, error) {
	addr, err := netmail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	return strings.ToLower(addr.Address), nil
}

func queryIntDefault(r *http.Request, name string, def int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// buildTextEmail builds a plain-text outbound message.
func buildTextEmail(from, to, subject, body string) []byte {
	date := time.Now().UTC().Format(time.RFC1123Z)
	return []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\n\r\n%s",
		from, to, subject, date, body))
}

// ServerAddr returns the configured listen address.
func (s *Server) ServerAddr() string {
	return s.Config.HTTP.Listen
}
