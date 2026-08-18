// Bounce management handlers (ADR 0019): a per-list view of members with
// bounce activity, where Owners can re-enable a bounced-out member or reset a
// member's bounce counter. Both actions are privileged membership changes and
// record Audit Events.
package server

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/barats/xlistman/internal/model"
)

// handleConsoleBounces lists members with bounce activity (bounce_count > 0 or
// Disabled), with the list's threshold, worst offenders first. Owners only.
func (s *Server) handleConsoleBounces(w http.ResponseWriter, r *http.Request, l *model.List) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	subs, err := s.Store.ListSubscriptions(ctx, l.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load members"})
		return
	}
	type bounceInfo struct {
		SubscriberID    int64  `json:"subscriber_id"`
		Email           string `json:"email"`
		Status          string `json:"status"`
		DeliveryMode    string `json:"delivery_mode"`
		BounceCount     int    `json:"bounce_count"`
		BounceThreshold int    `json:"bounce_threshold"`
	}
	result := []bounceInfo{}
	for _, sub := range subs {
		if sub.BounceCount <= 0 && sub.Status != model.SubscriptionStatusDisabled {
			continue
		}
		subscriber, err := s.Store.GetSubscriberByID(ctx, sub.SubscriberID)
		if err != nil {
			continue
		}
		result = append(result, bounceInfo{
			SubscriberID:    sub.SubscriberID,
			Email:           subscriber.Email,
			Status:          string(sub.Status),
			DeliveryMode:    string(sub.DeliveryMode),
			BounceCount:     sub.BounceCount,
			BounceThreshold: l.Settings.BounceThreshold,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].BounceCount != result[j].BounceCount {
			return result[i].BounceCount > result[j].BounceCount
		}
		return result[i].Email < result[j].Email
	})
	writeJSON(w, 200, result)
}

// handleConsoleBounceAction re-enables a Disabled member or resets a member's
// bounce count (POST /bounces/{subscriberID}/{re-enable|reset}). Owners only.
func (s *Server) handleConsoleBounceAction(w http.ResponseWriter, r *http.Request, l *model.List, subIDStr, action string) {
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
	subscr, err := s.Store.GetSubscription(ctx, l.ID, subID)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "member not found"})
		return
	}
	actor, _ := subscriberFrom(r)

	switch action {
	case "re-enable":
		if subscr.Status != model.SubscriptionStatusDisabled {
			writeJSON(w, 409, map[string]string{"error": "subscription is not disabled"})
			return
		}
		if err := s.Store.ReenableSubscription(ctx, subscr.ID); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to re-enable"})
			return
		}
		member, _ := s.Store.GetSubscriberByID(ctx, subID)
		s.audit(ctx, l, model.ActionMemberReenable, subscriberActor(actor), member.Email, "")
		writeJSON(w, 200, map[string]string{"status": "subscription re-enabled"})
	case "reset":
		if err := s.Store.ResetBounceCount(ctx, subscr.ID); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to reset bounce count"})
			return
		}
		member, _ := s.Store.GetSubscriberByID(ctx, subID)
		s.audit(ctx, l, model.ActionMemberResetBounces, subscriberActor(actor), member.Email, "")
		writeJSON(w, 200, map[string]string{"status": "bounce count reset"})
	default:
		http.NotFound(w, r)
	}
}
