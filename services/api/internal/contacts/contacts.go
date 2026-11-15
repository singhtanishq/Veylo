// Package contacts implements the contact graph: contact requests (with
// automatic mutual-accept), accepted contacts and blocking.
package contacts

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singhtanishq/veylo/services/api/internal/api"
	"github.com/singhtanishq/veylo/services/api/internal/notifications"
	"github.com/singhtanishq/veylo/services/api/internal/presence"
)

// Handler exposes contact endpoints.
type Handler struct {
	pool     *pgxpool.Pool
	presence *presence.Store
	notify   *notifications.Notifier
}

// NewHandler constructs the contacts handler.
func NewHandler(pool *pgxpool.Pool, p *presence.Store, n *notifications.Notifier) *Handler {
	return &Handler{pool: pool, presence: p, notify: n}
}

// Routes registers contact endpoints (auth applied by router).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/contacts", h.list)
	r.Post("/contacts/requests", h.createRequest)
	r.Get("/contacts/requests", h.listRequests)
	r.Post("/contacts/requests/{requestID}/accept", h.acceptRequest)
	r.Post("/contacts/requests/{requestID}/decline", h.declineRequest)
	r.Delete("/contacts/{userID}", h.removeContact)
	r.Get("/contacts/blocks", h.listBlocks)
	r.Post("/contacts/blocks", h.block)
	r.Delete("/contacts/blocks/{userID}", h.unblock)
}

// ---------------------------------------------------------------------------
// Contact projections
// ---------------------------------------------------------------------------

type contactUser struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	About       *string `json:"about"`
	AvatarURL   *string `json:"avatar_url"`
	Online      bool    `json:"online"`
	LastSeenAt  *string `json:"last_seen_at"`
	AddedAt     string  `json:"added_at,omitempty"`
}

type contactRequestView struct {
	ID        string      `json:"id"`
	User      contactUser `json:"user"`
	Message   *string     `json:"message"`
	CreatedAt string      `json:"created_at"`
}

// loadUser projects any active user with privacy-respecting presence fields.
func (h *Handler) loadUser(r *http.Request, id, viewer uuid.UUID) (contactUser, error) {
	var cu contactUser
	var avatarID *uuid.UUID
	var lastSeen *time.Time
	var createdAt time.Time
	var onlineVis, lastSeenVis string

	err := h.pool.QueryRow(r.Context(), `
		SELECT u.id, u.username, p.display_name, p.about, p.avatar_attachment_id, u.last_seen_at, u.created_at,
		       s.online_visibility, s.last_seen_visibility
		FROM users u
		JOIN user_profiles p ON p.user_id = u.id
		JOIN user_privacy_settings s ON s.user_id = u.id
		WHERE u.id = $1 AND u.account_status = 'active'
	`, id).Scan(&cu.ID, &cu.Username, &cu.DisplayName, &cu.About, &avatarID, &lastSeen,
		&createdAt, &onlineVis, &lastSeenVis)
	if err != nil {
		return cu, err
	}
	if avatarID != nil {
		url := "/v1/attachments/" + avatarID.String() + "/download"
		cu.AvatarURL = &url
	}

	// Presence is exposed to contacts per the target's privacy settings.
	viewerSees := onlineVis == "everyone" ||
		(onlineVis == "contacts" && viewer != id && h.isContact(r, viewer, id))
	if viewerSees {
		cu.Online = h.presence.IsOnline(r.Context(), id)
	}
	if lastSeen != nil && viewerSees {
		ts := lastSeen.UTC().Format(time.RFC3339)
		cu.LastSeenAt = &ts
	}
	return cu, nil
}

func (h *Handler) isContact(r *http.Request, a, b uuid.UUID) bool {
	var ok bool
	_ = h.pool.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM contacts WHERE owner_id = $1 AND contact_id = $2) OR
		        EXISTS(SELECT 1 FROM contacts WHERE owner_id = $2 AND contact_id = $1)`, a, b).Scan(&ok)
	return ok
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	me := api.UserID(r.Context())
	rows, err := h.pool.Query(r.Context(), `
		SELECT c.contact_id, c.created_at FROM contacts c
		JOIN users u ON u.id = c.contact_id AND u.account_status = 'active'
		WHERE c.owner_id = $1
		ORDER BY c.created_at DESC
	`, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	type row struct {
		id  uuid.UUID
		add time.Time
	}
	var rowsData []row
	for rows.Next() {
		var rr row
		if err := rows.Scan(&rr.id, &rr.add); err != nil {
			api.WriteError(w, r, err)
			return
		}
		rowsData = append(rowsData, rr)
	}

	out := []contactUser{}
	for _, rr := range rowsData {
		cu, err := h.loadUser(r, rr.id, me)
		if err != nil {
			continue
		}
		cu.AddedAt = rr.add.UTC().Format(time.RFC3339)
		out = append(out, cu)
	}
	api.WriteJSON(w, r, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// Requests
// ---------------------------------------------------------------------------

func (h *Handler) createRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID  string  `json:"user_id"`
		Message *string `json:"message"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	target, err := uuid.Parse(req.UserID)
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	if target == me {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"You cannot add yourself as a contact.", nil))
		return
	}
	ctx := r.Context()

	// Target must be active.
	var status string
	if err := h.pool.QueryRow(ctx, `SELECT account_status FROM users WHERE id = $1`, target).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.WriteError(w, r, api.ErrNotFound)
			return
		}
		api.WriteError(w, r, err)
		return
	}
	if status != "active" {
		api.WriteError(w, r, api.ErrNotFound)
		return
	}

	// Block checks (either direction).
	var blocked bool
	if err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM blocked_users WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))
	`, me, target).Scan(&blocked); err == nil && blocked {
		api.WriteError(w, r, api.ErrBlockedBy)
		return
	}

	// Already contacts?
	var existing bool
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM contacts WHERE owner_id=$1 AND contact_id=$2)`, me, target).Scan(&existing); err == nil && existing {
		api.WriteError(w, r, api.Errf(http.StatusConflict, api.CodeConflict, "You are already contacts.", nil))
		return
	}

	// Respect target's who_can_send_requests setting.
	var whoCan string
	if err := h.pool.QueryRow(ctx,
		`SELECT who_can_send_requests FROM user_privacy_settings WHERE user_id = $1`, target).Scan(&whoCan); err == nil {
		if whoCan == "nobody" {
			api.WriteError(w, r, api.Errf(http.StatusForbidden, api.CodeForbidden,
				"This user is not accepting contact requests.", nil))
			return
		}
	}

	// If the target already requested me, accept both sides atomically.
	var reverseID uuid.UUID
	err = h.pool.QueryRow(ctx, `
		SELECT id FROM contact_requests
		WHERE requester_id = $2 AND addressee_id = $1 AND status = 'pending'
	`, me, target).Scan(&reverseID)
	if err == nil {
		tx, err := h.pool.Begin(ctx)
		if err != nil {
			api.WriteError(w, r, err)
			return
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx,
			`UPDATE contact_requests SET status='accepted', responded_at=now() WHERE id = $1`, reverseID); err != nil {
			api.WriteError(w, r, err)
			return
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO contacts (owner_id, contact_id) VALUES ($1,$2), ($2,$1) ON CONFLICT DO NOTHING`, me, target); err != nil {
			api.WriteError(w, r, err)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			api.WriteError(w, r, err)
			return
		}
		h.notify.Notify(ctx, target, "contact_accepted", "Contact added",
			"Your contact request was accepted.", nil, nil, &me)
		h.notify.Notify(ctx, me, "contact_accepted", "Contact added",
			"You are now contacts.", nil, nil, &target)
		api.WriteJSON(w, r, http.StatusOK, map[string]any{"status": "accepted"})
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		api.WriteError(w, r, err)
		return
	}

	var requestID uuid.UUID
	err = h.pool.QueryRow(ctx, `
		INSERT INTO contact_requests (requester_id, addressee_id, message)
		VALUES ($1, $2, NULLIF($3,''))
		ON CONFLICT (requester_id, addressee_id) DO UPDATE
			SET status = 'pending', message = EXCLUDED.message, created_at = now(), responded_at = NULL
		RETURNING id
	`, me, target, derefString(req.Message)).Scan(&requestID)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}

	h.notify.Notify(ctx, target, "contact_request", "New contact request",
		derefString(req.Message), nil, nil, &me)
	api.WriteJSON(w, r, http.StatusCreated, map[string]any{"id": requestID, "status": "pending"})
}

func (h *Handler) listRequests(w http.ResponseWriter, r *http.Request) {
	me := api.UserID(r.Context())
	direction := r.URL.Query().Get("direction")
	if direction == "" {
		direction = "incoming"
	}
	var rows pgx.Rows
	var err error
	if direction == "outgoing" {
		rows, err = h.pool.Query(r.Context(), `
			SELECT id, requester_id, message, created_at FROM contact_requests
			WHERE requester_id = $1 AND status = 'pending' ORDER BY created_at DESC
		`, me)
	} else {
		rows, err = h.pool.Query(r.Context(), `
			SELECT id, requester_id, message, created_at FROM contact_requests
			WHERE addressee_id = $1 AND status = 'pending' ORDER BY created_at DESC
		`, me)
	}
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	type row struct {
		id, uid   uuid.UUID
		msg       *string
		createdAt time.Time
	}
	var data []row
	for rows.Next() {
		var rr row
		if err := rows.Scan(&rr.id, &rr.uid, &rr.msg, &rr.createdAt); err != nil {
			api.WriteError(w, r, err)
			return
		}
		data = append(data, rr)
	}

	out := []contactRequestView{}
	for _, rr := range data {
		cu, err := h.loadUser(r, rr.uid, me)
		if err != nil {
			continue
		}
		out = append(out, contactRequestView{
			ID:        rr.id.String(),
			User:      cu,
			Message:   rr.msg,
			CreatedAt: rr.createdAt.UTC().Format(time.RFC3339),
		})
	}
	api.WriteJSON(w, r, http.StatusOK, out)
}

func (h *Handler) acceptRequest(w http.ResponseWriter, r *http.Request) {
	h.respondToRequest(w, r, "accepted")
}

func (h *Handler) declineRequest(w http.ResponseWriter, r *http.Request) {
	h.respondToRequest(w, r, "declined")
}

func (h *Handler) respondToRequest(w http.ResponseWriter, r *http.Request, decision string) {
	id, err := uuid.Parse(chi.URLParam(r, "requestID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	ctx := r.Context()

	var requester uuid.UUID
	var status string
	err = h.pool.QueryRow(ctx,
		`SELECT requester_id, status FROM contact_requests WHERE id = $1 AND addressee_id = $2`, id, me).
		Scan(&requester, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.WriteError(w, r, api.ErrNotFound)
			return
		}
		api.WriteError(w, r, err)
		return
	}
	if status != "pending" {
		api.WriteError(w, r, api.Errf(http.StatusConflict, api.CodeConflict, "This request was already handled.", nil))
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`UPDATE contact_requests SET status = $1, responded_at = now() WHERE id = $2`, decision, id); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if decision == "accepted" {
		if _, err := tx.Exec(ctx,
			`INSERT INTO contacts (owner_id, contact_id) VALUES ($1,$2), ($2,$1) ON CONFLICT DO NOTHING`,
			me, requester); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		api.WriteError(w, r, err)
		return
	}

	if decision == "accepted" {
		h.notify.Notify(ctx, requester, "contact_accepted", "Contact added",
			"Your contact request was accepted.", nil, nil, &me)
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"status": decision})
}

func (h *Handler) removeContact(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	tag, err := h.pool.Exec(r.Context(),
		`DELETE FROM contacts WHERE (owner_id = $1 AND contact_id = $2) OR (owner_id = $2 AND contact_id = $1)`, me, id)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		api.WriteError(w, r, api.ErrNotFound)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"removed": true})
}

// ---------------------------------------------------------------------------
// Blocking
// ---------------------------------------------------------------------------

func (h *Handler) listBlocks(w http.ResponseWriter, r *http.Request) {
	me := api.UserID(r.Context())
	rows, err := h.pool.Query(r.Context(), `
		SELECT b.blocked_id FROM blocked_users b
		JOIN users u ON u.id = b.blocked_id
		WHERE b.blocker_id = $1
		ORDER BY b.created_at DESC
	`, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			api.WriteError(w, r, err)
			return
		}
		ids = append(ids, id)
	}
	out := []contactUser{}
	for _, id := range ids {
		cu, err := h.loadUser(r, id, me)
		if err != nil {
			continue
		}
		out = append(out, cu)
	}
	api.WriteJSON(w, r, http.StatusOK, out)
}

func (h *Handler) block(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	target, err := uuid.Parse(req.UserID)
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	if target == me {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"You cannot block yourself.", nil))
		return
	}
	if _, err := h.pool.Exec(r.Context(),
		`INSERT INTO blocked_users (blocker_id, blocked_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, me, target); err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"blocked": true})
}

func (h *Handler) unblock(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	tag, err := h.pool.Exec(r.Context(),
		`DELETE FROM blocked_users WHERE blocker_id = $1 AND blocked_id = $2`, me, id)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		api.WriteError(w, r, api.ErrNotFound)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"unblocked": true})
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
