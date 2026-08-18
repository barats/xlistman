// Server-administration handlers (ADR 0017): the instance-wide Administrator
// privilege. An Administrator (a Subscriber designated via CLI) can create
// domains and lists, manage other Administrators, delete lists, and change
// ListType, from the web console. The CLI retains parity commands sharing the
// same store functions.
package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/barats/xlistman/internal/model"
)

var (
	domainNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	listNameRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)
)

func (s *Server) registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/console/admin/info", s.requireAuth(s.requireManagement(s.handleAdminInfo)))
	mux.HandleFunc("/api/console/admin/domains", s.requireAuth(s.requireManagement(s.requireAdmin(s.handleAdminDomains))))
	mux.HandleFunc("/api/console/admin/lists", s.requireAuth(s.requireManagement(s.requireAdmin(s.handleAdminLists))))
	mux.HandleFunc("/api/console/admin/lists/", s.requireAuth(s.requireManagement(s.requireAdmin(s.handleAdminListAction))))
	mux.HandleFunc("/api/console/admin/administrators", s.requireAuth(s.requireManagement(s.requireAdmin(s.handleAdminAdministrators))))
	mux.HandleFunc("/api/console/admin/administrators/", s.requireAuth(s.requireManagement(s.requireAdmin(s.handleAdminAdministratorRemoveHandler))))
	mux.HandleFunc("/api/console/admin/audit", s.requireAuth(s.requireManagement(s.requireAdmin(s.handleAdminAudit))))
}

// handleAdminInfo reports whether the signed-in Subscriber holds the
// instance-wide Administrator privilege, so the UI can show or hide the
// server-admin area. Open to any authenticated Subscriber.
func (s *Server) handleAdminInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sub, ok := subscriberFrom(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	isAdmin, _ := s.Store.IsAdministrator(r.Context(), sub.ID)
	writeJSON(w, 200, map[string]any{"is_administrator": isAdmin})
}

// requireAdmin gates a handler on the caller holding the Administrator
// privilege.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sub, ok := subscriberFrom(r)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		isAdmin, err := s.Store.IsAdministrator(r.Context(), sub.ID)
		if err != nil || !isAdmin {
			writeJSON(w, 403, map[string]string{"error": "administrator required"})
			return
		}
		next(w, r)
	}
}

// handleAdminDomains lists every hosted domain, or creates one on POST.
func (s *Server) handleAdminDomains(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method == http.MethodPost {
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request body"})
			return
		}
		name := strings.ToLower(strings.TrimSpace(body.Name))
		if !validDomainName(name) {
			writeJSON(w, 400, map[string]string{"error": "invalid domain name (use a lowercase hostname like example.com)"})
			return
		}
		if _, err := s.Store.CreateDomain(ctx, name, strings.TrimSpace(body.Description)); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				writeJSON(w, 409, map[string]string{"error": "domain already exists"})
				return
			}
			writeJSON(w, 500, map[string]string{"error": "failed to create domain"})
			return
		}
		actor, _ := subscriberFrom(r)
		s.audit(ctx, nil, model.ActionDomainCreate, subscriberActor(actor), name, strings.TrimSpace(body.Description))
		writeJSON(w, 201, map[string]string{"status": "domain created"})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	domains, err := s.Store.ListDomains(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load domains"})
		return
	}
	lists, err := s.Store.ListLists(ctx, "")
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load lists"})
		return
	}
	listCount := map[string]int{}
	for _, l := range lists {
		listCount[l.Domain]++
	}
	type domainInfo struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		ListCount   int    `json:"list_count"`
	}
	result := make([]domainInfo, 0, len(domains))
	for _, d := range domains {
		result = append(result, domainInfo{ID: d.ID, Name: d.Name, Description: d.Description, ListCount: listCount[d.Name]})
	}
	writeJSON(w, 200, result)
}

// handleAdminLists lists every list on the instance, or creates one on POST.
func (s *Server) handleAdminLists(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method == http.MethodPost {
		var body struct {
			ListName        string `json:"list_name"`
			Domain          string `json:"domain"`
			ListType        string `json:"list_type"`
			Description     string `json:"description"`
			FirstOwnerEmail string `json:"first_owner_email"`
			Moderation      bool   `json:"moderation"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request body"})
			return
		}
		listName := strings.ToLower(strings.TrimSpace(body.ListName))
		domain := strings.ToLower(strings.TrimSpace(body.Domain))
		listType := model.ListType(body.ListType)
		if !validListName(listName) {
			writeJSON(w, 400, map[string]string{"error": "invalid list name (use letters, digits, dots, dashes, underscores)"})
			return
		}
		if listType != model.ListTypeDiscussion && listType != model.ListTypeNewsletter {
			writeJSON(w, 400, map[string]string{"error": "list type must be discussion or newsletter"})
			return
		}
		d, err := s.Store.GetDomain(ctx, domain)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "domain not found: " + domain + ". Create it first in Domains."})
			return
		}
		l, err := s.Store.CreateList(ctx, listName, d.ID, d.Name, strings.TrimSpace(body.Description), listType)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				writeJSON(w, 409, map[string]string{"error": "a list with this address already exists"})
				return
			}
			s.Logger.Error("admin create list", "error", err)
			writeJSON(w, 500, map[string]string{"error": "failed to create list"})
			return
		}
		if listType == model.ListTypeDiscussion && body.Moderation {
			settings := l.Settings
			settings.ModerationEnabled = true
			if err := s.Store.UpdateListSettings(ctx, l.ID, settings); err != nil {
				s.Logger.Error("admin set moderation", "error", err)
			}
		}
		// First owner: the creating Administrator by default, overridable to
		// any known Subscriber (ADR 0017).
		ownerEmail := strings.TrimSpace(body.FirstOwnerEmail)
		if ownerEmail == "" {
			creator, _ := subscriberFrom(r)
			if creator != nil {
				ownerEmail = creator.Email
			}
		}
		if ownerEmail != "" {
			if owner, err := s.Store.GetOrCreateSubscriber(ctx, ownerEmail); err == nil {
				if err := s.Store.AddOwner(ctx, l.ID, owner.ID); err != nil {
					s.Logger.Error("admin add first owner", "error", err)
				}
			}
		}
		actor, _ := subscriberFrom(r)
		s.audit(ctx, l, model.ActionListCreate, subscriberActor(actor), l.Address(), string(l.ListType))
		writeJSON(w, 201, map[string]any{"status": "list created", "address": l.Address()})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lists, err := s.Store.ListLists(ctx, "")
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load lists"})
		return
	}
	type listInfo struct {
		Address     string `json:"address"`
		ListName    string `json:"list_name"`
		Domain      string `json:"domain"`
		ListType    string `json:"list_type"`
		Description string `json:"description"`
		MemberCount int    `json:"member_count"`
	}
	result := make([]listInfo, 0, len(lists))
	for _, l := range lists {
		subs, _ := s.Store.ListSubscriptions(ctx, l.ID)
		result = append(result, listInfo{
			Address:     l.Address(),
			ListName:    l.ListName,
			Domain:      l.Domain,
			ListType:    string(l.ListType),
			Description: l.Description,
			MemberCount: len(subs),
		})
	}
	writeJSON(w, 200, result)
}

// handleAdminListAction handles DELETE (delete the list, hard) and
// POST .../type (change ListType) on {domain}/{listname}.
func (s *Server) handleAdminListAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/console/admin/lists/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	domain := parts[0]
	listName := parts[1]
	if _, err := s.Store.GetList(r.Context(), listName, domain); err != nil {
		writeJSON(w, 404, map[string]string{"error": "list not found"})
		return
	}

	switch {
	case len(parts) == 2 && r.Method == http.MethodDelete:
		s.handleAdminListDelete(w, r, listName, domain)
	case len(parts) == 3 && parts[2] == "type" && r.Method == http.MethodPost:
		s.handleAdminListType(w, r, listName, domain)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleAdminListDelete(w http.ResponseWriter, r *http.Request, listName, domain string) {
	ctx := r.Context()
	admin, _ := subscriberFrom(r)
	l, err := s.Store.GetList(ctx, listName, domain)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "list not found"})
		return
	}
	// Hard delete: the store removes the list and every related row (archive,
	// held, subscriptions, roles, queue) in one transaction. Audit events are
	// not removed, so a deleted list's history stays visible to Administrators.
	if err := s.Store.DeleteList(ctx, listName, domain); err != nil {
		s.Logger.Error("admin delete list", "list", listName+"@"+domain, "error", err)
		writeJSON(w, 500, map[string]string{"error": "failed to delete list"})
		return
	}
	if admin != nil {
		s.Logger.Info("list deleted", "list", listName+"@"+domain, "by", admin.Email)
	}
	s.audit(ctx, l, model.ActionListDelete, subscriberActor(admin), l.Address(), "")
	writeJSON(w, 200, map[string]string{"status": "list deleted"})
}

func (s *Server) handleAdminListType(w http.ResponseWriter, r *http.Request, listName, domain string) {
	var body struct {
		ListType string `json:"list_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	listType := model.ListType(body.ListType)
	if listType != model.ListTypeDiscussion && listType != model.ListTypeNewsletter {
		writeJSON(w, 400, map[string]string{"error": "list type must be discussion or newsletter"})
		return
	}
	l, err := s.Store.GetList(r.Context(), listName, domain)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "list not found"})
		return
	}
	if err := s.Store.UpdateListType(r.Context(), l.ID, listType); err != nil {
		s.Logger.Error("admin change list type", "list", l.Address(), "error", err)
		writeJSON(w, 500, map[string]string{"error": "failed to change list type"})
		return
	}
	actor, _ := subscriberFrom(r)
	s.audit(r.Context(), l, model.ActionListType, subscriberActor(actor), l.Address(), string(listType))
	writeJSON(w, 200, map[string]string{"status": "list type updated"})
}

// handleAdminAdministrators lists Administrators, or designates one on POST.
// Designation is Subscriber-first: only a known Subscriber can become an
// Administrator.
func (s *Server) handleAdminAdministrators(w http.ResponseWriter, r *http.Request) {
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
		sub, err := s.Store.GetSubscriber(ctx, email)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "unknown subscriber: " + email + ". They must be a known Subscriber first (subscribe to a list or be added by an owner)."})
			return
		}
		if err := s.Store.AddAdministrator(ctx, sub.ID); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to add administrator"})
			return
		}
		actor, _ := subscriberFrom(r)
		s.audit(ctx, nil, model.ActionAdminDesignate, subscriberActor(actor), email, "")
		writeJSON(w, 201, map[string]string{"status": "administrator added"})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	admins, err := s.Store.ListAdministrators(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load administrators"})
		return
	}
	type adminInfo struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}
	result := make([]adminInfo, 0, len(admins))
	for _, a := range admins {
		sub, err := s.Store.GetSubscriberByID(ctx, a.SubscriberID)
		if err != nil {
			continue
		}
		result = append(result, adminInfo{ID: sub.ID, Email: sub.Email})
	}
	writeJSON(w, 200, result)
}

// handleAdminAdministratorRemove revokes the Administrator privilege
// (DELETE /administrators/{subscriberID}).
func (s *Server) handleAdminAdministratorRemoveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/console/admin/administrators/")
	subID, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sub, _ := s.Store.GetSubscriberByID(r.Context(), subID)
	email := ""
	if sub != nil {
		email = sub.Email
	}
	if err := s.Store.RemoveAdministrator(r.Context(), subID); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to remove administrator"})
		return
	}
	actor, _ := subscriberFrom(r)
	s.audit(r.Context(), nil, model.ActionAdminRevoke, subscriberActor(actor), email, "")
	writeJSON(w, 200, map[string]string{"status": "administrator removed"})
}

func validDomainName(name string) bool {
	return name != "" && strings.Contains(name, ".") && domainNameRe.MatchString(name)
}

func validListName(name string) bool {
	return name != "" && listNameRe.MatchString(name)
}
