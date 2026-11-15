// Package settings exposes privacy and notification preference management.
package settings

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singhtanishq/veylo/services/api/internal/api"
)

// Handler exposes settings endpoints.
type Handler struct {
	pool *pgxpool.Pool
}

// NewHandler constructs the settings handler.
func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

// Routes registers settings endpoints (auth applied by router).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/settings/privacy", h.getPrivacy)
	r.Patch("/settings/privacy", h.updatePrivacy)
	r.Get("/settings/notifications", h.getNotifications)
	r.Patch("/settings/notifications", h.updateNotifications)
}

// ---------------------------------------------------------------------------
// Privacy
// ---------------------------------------------------------------------------

type privacySettings struct {
	UserID              string `json:"-"`
	LastSeenVisibility  string `json:"last_seen_visibility"`
	OnlineVisibility    string `json:"online_visibility"`
	ProfileVisibility   string `json:"profile_visibility"`
	ReadReceiptsEnabled bool   `json:"read_receipts_enabled"`
	DiscoverableByEmail bool   `json:"discoverable_by_email"`
	DiscoverableByPhone bool   `json:"discoverable_by_phone"`
	WhoCanSendRequests  string `json:"who_can_send_requests"`
}

func (h *Handler) getPrivacy(w http.ResponseWriter, r *http.Request) {
	var s privacySettings
	err := h.pool.QueryRow(r.Context(), `
		SELECT user_id, last_seen_visibility, online_visibility, profile_visibility,
		       read_receipts_enabled, discoverable_by_email, discoverable_by_phone, who_can_send_requests
		FROM user_privacy_settings WHERE user_id = $1
	`, api.UserID(r.Context())).Scan(&s.UserID, &s.LastSeenVisibility, &s.OnlineVisibility,
		&s.ProfileVisibility, &s.ReadReceiptsEnabled, &s.DiscoverableByEmail, &s.DiscoverableByPhone,
		&s.WhoCanSendRequests)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, s)
}

func (h *Handler) updatePrivacy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LastSeenVisibility  *string `json:"last_seen_visibility"`
		OnlineVisibility    *string `json:"online_visibility"`
		ProfileVisibility   *string `json:"profile_visibility"`
		ReadReceiptsEnabled *bool   `json:"read_receipts_enabled"`
		DiscoverableByEmail *bool   `json:"discoverable_by_email"`
		DiscoverableByPhone *bool   `json:"discoverable_by_phone"`
		WhoCanSendRequests  *string `json:"who_can_send_requests"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	me := api.UserID(r.Context())

	triple := func(v *string, field string) (string, bool) {
		if v == nil {
			return "", true
		}
		switch *v {
		case "everyone", "contacts", "nobody":
			return *v, true
		default:
			return "", false
		}
	}
	_ = triple

	if req.LastSeenVisibility != nil {
		if !inSet(*req.LastSeenVisibility, "everyone", "contacts", "nobody") {
			api.WriteError(w, r, api.ErrValidation)
			return
		}
	}
	if req.OnlineVisibility != nil {
		if !inSet(*req.OnlineVisibility, "everyone", "contacts", "nobody") {
			api.WriteError(w, r, api.ErrValidation)
			return
		}
	}
	if req.ProfileVisibility != nil {
		if !inSet(*req.ProfileVisibility, "everyone", "contacts") {
			api.WriteError(w, r, api.ErrValidation)
			return
		}
	}
	if req.WhoCanSendRequests != nil {
		if !inSet(*req.WhoCanSendRequests, "everyone", "contacts_of_contacts", "nobody") {
			api.WriteError(w, r, api.ErrValidation)
			return
		}
	}

	type pair struct {
		sql string
		val any
	}
	updates := []pair{}
	if req.LastSeenVisibility != nil {
		updates = append(updates, pair{`last_seen_visibility = $2`, *req.LastSeenVisibility})
	}
	if req.OnlineVisibility != nil {
		updates = append(updates, pair{`online_visibility = $2`, *req.OnlineVisibility})
	}
	if req.ProfileVisibility != nil {
		updates = append(updates, pair{`profile_visibility = $2`, *req.ProfileVisibility})
	}
	if req.ReadReceiptsEnabled != nil {
		updates = append(updates, pair{`read_receipts_enabled = $2`, *req.ReadReceiptsEnabled})
	}
	if req.DiscoverableByEmail != nil {
		updates = append(updates, pair{`discoverable_by_email = $2`, *req.DiscoverableByEmail})
	}
	if req.DiscoverableByPhone != nil {
		updates = append(updates, pair{`discoverable_by_phone = $2`, *req.DiscoverableByPhone})
	}
	if req.WhoCanSendRequests != nil {
		updates = append(updates, pair{`who_can_send_requests = $2`, *req.WhoCanSendRequests})
	}

	for _, u := range updates {
		if _, err := h.pool.Exec(r.Context(),
			`UPDATE user_privacy_settings SET `+u.sql+`, updated_at = now() WHERE user_id = $1`, me, u.val); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}
	h.getPrivacy(w, r)
}

// ---------------------------------------------------------------------------
// Notifications
// ---------------------------------------------------------------------------

type notificationSettings struct {
	UserID                string `json:"-"`
	NotifyMessages        bool   `json:"notify_messages"`
	NotifyMentions        bool   `json:"notify_mentions"`
	NotifyContactRequests bool   `json:"notify_contact_requests"`
	NotifyGroupInvites    bool   `json:"notify_group_invites"`
	Theme                 string `json:"theme"`
}

func (h *Handler) getNotifications(w http.ResponseWriter, r *http.Request) {
	var s notificationSettings
	err := h.pool.QueryRow(r.Context(), `
		SELECT user_id, notify_messages, notify_mentions, notify_contact_requests, notify_group_invites, theme
		FROM user_preferences WHERE user_id = $1
	`, api.UserID(r.Context())).Scan(&s.UserID, &s.NotifyMessages, &s.NotifyMentions,
		&s.NotifyContactRequests, &s.NotifyGroupInvites, &s.Theme)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, s)
}

func (h *Handler) updateNotifications(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NotifyMessages        *bool   `json:"notify_messages"`
		NotifyMentions        *bool   `json:"notify_mentions"`
		NotifyContactRequests *bool   `json:"notify_contact_requests"`
		NotifyGroupInvites    *bool   `json:"notify_group_invites"`
		Theme                 *string `json:"theme"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	me := api.UserID(r.Context())

	if req.Theme != nil && !inSet(*req.Theme, "light", "dark", "system") {
		api.WriteError(w, r, api.ErrValidation)
		return
	}

	type pair struct {
		sql string
		val any
	}
	updates := []pair{}
	if req.NotifyMessages != nil {
		updates = append(updates, pair{`notify_messages = $2`, *req.NotifyMessages})
	}
	if req.NotifyMentions != nil {
		updates = append(updates, pair{`notify_mentions = $2`, *req.NotifyMentions})
	}
	if req.NotifyContactRequests != nil {
		updates = append(updates, pair{`notify_contact_requests = $2`, *req.NotifyContactRequests})
	}
	if req.NotifyGroupInvites != nil {
		updates = append(updates, pair{`notify_group_invites = $2`, *req.NotifyGroupInvites})
	}
	if req.Theme != nil {
		updates = append(updates, pair{`theme = $2`, *req.Theme})
	}

	for _, u := range updates {
		if _, err := h.pool.Exec(r.Context(),
			`UPDATE user_preferences SET `+u.sql+`, updated_at = now() WHERE user_id = $1`, me, u.val); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}
	h.getNotifications(w, r)
}

func inSet(v string, set ...string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}
