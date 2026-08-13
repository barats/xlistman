// Package server implements the xListman HTTP server (API + health + metrics).
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/barat/xlistman/internal/config"
	"github.com/barat/xlistman/internal/store"
)

// Server is the HTTP server for xListman.
type Server struct {
	Store    store.Store
	Config   *config.Config
	Logger   *slog.Logger
	httpSrv  *http.Server
}

// New creates a new HTTP server.
func New(cfg *config.Config, st store.Store, logger *slog.Logger) *Server {
	s := &Server{
		Store:  st,
		Config: cfg,
		Logger: logger,
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.httpSrv = &http.Server{
		Addr:         cfg.HTTP.Listen,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	return s
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

func (s *Server) handleListDetail(w http.ResponseWriter, r *http.Request) {
	// Path: /api/lists/{domain}/{listname} or /api/lists/{domain}/{listname}/subscribe
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

	if len(parts) >= 3 && parts[2] == "subscribe" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleSubscribe(w, r, l)
		return
	}

	// List detail
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

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request, l interface{}) {
	// TODO: implement subscription flow (create subscriber, send confirmation)
	writeJSON(w, 202, map[string]string{"status": "confirmation email sent"})
}

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
	if body.Email == "" {
		writeJSON(w, 400, map[string]string{"error": "email is required"})
		return
	}
	// TODO: create magic link and send email
	writeJSON(w, 202, map[string]string{"status": "magic link sent"})
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeJSON(w, 400, map[string]string{"error": "token is required"})
		return
	}
	// TODO: verify magic link token and create session
	writeJSON(w, 200, map[string]string{"status": "verified"})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// ServerAddr returns the configured listen address.
func (s *Server) ServerAddr() string {
	return s.Config.HTTP.Listen
}
