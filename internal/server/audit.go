// Audit trail handlers and helpers (ADR 0018). The per-list audit view lives
// in the role console (Owners only); the instance-wide view lives in the
// server-admin area (Administrators only).
package server

import (
	"context"
	"net/http"

	"github.com/barats/xlistman/internal/model"
)

// subscriberActor wraps a signed-in Subscriber as the actor for an Audit Event.
// A nil subscriber (should not happen in gated handlers) falls back to the CLI
// actor so the audit write never panics.
func subscriberActor(sub *model.Subscriber) model.AuditActor {
	if sub == nil {
		return model.AuditActor{Kind: model.AuditActorCLI}
	}
	return model.AuditActor{Kind: model.AuditActorSubscriber, ID: sub.ID, Email: sub.Email}
}

// audit records an Audit Event for a store-direct action (ADR 0018). The state
// change is already committed; a failed write is logged loudly rather than
// rolled back, since the schema has no cross-row transaction spanning the two.
func (s *Server) audit(ctx context.Context, l *model.List, action string, actor model.AuditActor, target, detail string) {
	var listID *int64
	listAddr := ""
	if l != nil {
		id := l.ID
		listID = &id
		listAddr = l.Address()
	}
	e := model.NewAuditEvent(listID, listAddr, action, actor, target, detail)
	if err := s.Store.CreateAuditEvent(ctx, e); err != nil {
		s.Logger.Error("record audit", "action", action, "error", err)
	}
}

// handleConsoleAudit lists a list's Audit Events (Owners only, via the
// requireOwner gate in console.go). Optional ?action= filter.
func (s *Server) handleConsoleAudit(w http.ResponseWriter, r *http.Request, l *model.List) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	listID := l.ID
	s.writeAuditEvents(w, r, &listID, r.URL.Query().Get("action"))
}

// handleAdminAudit lists every Audit Event instance-wide (Administrators
// only). Optional ?action= filter.
func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writeAuditEvents(w, r, nil, r.URL.Query().Get("action"))
}

// writeAuditEvents writes Audit Events newest-first as JSON.
func (s *Server) writeAuditEvents(w http.ResponseWriter, r *http.Request, listID *int64, action string) {
	events, err := s.Store.ListAuditEvents(r.Context(), listID, action, 0)
	if err != nil {
		s.Logger.Error("list audit events", "error", err)
		writeJSON(w, 500, map[string]string{"error": "failed to load audit events"})
		return
	}
	type auditInfo struct {
		ID          int64  `json:"id"`
		At          string `json:"at"`
		ListAddr    string `json:"list_addr,omitempty"`
		Action      string `json:"action"`
		ActorKind   string `json:"actor_kind"`
		ActorEmail  string `json:"actor_email,omitempty"`
		ActorDetail string `json:"actor_detail,omitempty"`
		Target      string `json:"target"`
		Detail      string `json:"detail"`
	}
	result := make([]auditInfo, 0, len(events))
	for _, e := range events {
		result = append(result, auditInfo{
			ID:          e.ID,
			At:          e.At.UTC().Format("2006-01-02T15:04:05Z"),
			ListAddr:    e.ListAddr,
			Action:      e.Action,
			ActorKind:   e.ActorKind,
			ActorEmail:  e.ActorEmail,
			ActorDetail: e.ActorDetail,
			Target:      e.Target,
			Detail:      e.Detail,
		})
	}
	writeJSON(w, 200, result)
}
