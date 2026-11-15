// Package conversations implements one-to-one and group conversations:
// duplicate-safe direct creation, group lifecycle, membership with roles,
// per-member settings (mute/pin/archive) and invitations.
package conversations

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singhtanishq/veylo/services/api/internal/api"
	"github.com/singhtanishq/veylo/services/api/internal/notifications"
	"github.com/singhtanishq/veylo/services/api/internal/presence"
)

// Hub is the realtime fanout surface conversations need.
type Hub interface {
	ToUsers(userIDs []uuid.UUID, payload any, excludeConn string)
	IsOnline(userID uuid.UUID) bool
}

// Handler exposes conversation endpoints.
type Handler struct {
	pool     *pgxpool.Pool
	hub      Hub
	presence *presence.Store
	notify   *notifications.Notifier
}

// NewHandler constructs the conversations handler.
func NewHandler(pool *pgxpool.Pool, hub Hub, p *presence.Store, n *notifications.Notifier) *Handler {
	return &Handler{pool: pool, hub: hub, presence: p, notify: n}
}

// SetHub wires the realtime hub after construction (breaks the dependency
// cycle: hub needs messages, messages needs conversations).
func (h *Handler) SetHub(hub Hub) { h.hub = hub }

// Routes registers conversation endpoints (auth applied by router).
func (h *Handler) Routes(r chi.Router) {
	r.Post("/conversations/direct", h.createDirect)
	r.Post("/conversations/group", h.createGroup)
	r.Get("/conversations", h.list)
	r.Get("/conversations/{conversationID}", h.details)
	r.Patch("/conversations/{conversationID}", h.update)
	r.Delete("/conversations/{conversationID}", h.deleteGroup)
	r.Patch("/conversations/{conversationID}/settings", h.updateSettings)
	r.Post("/conversations/{conversationID}/members", h.addMembers)
	r.Delete("/conversations/{conversationID}/members/me", h.leave)
	r.Delete("/conversations/{conversationID}/members/{userID}", h.removeMember)
	r.Patch("/conversations/{conversationID}/members/{userID}/role", h.changeRole)
	r.Post("/conversations/{conversationID}/invitations", h.invite)
	r.Post("/conversations/{conversationID}/invitations/{invitationID}/accept", h.acceptInvitation)
	r.Post("/conversations/{conversationID}/invitations/{invitationID}/decline", h.declineInvitation)
	r.Get("/conversations/{conversationID}/invitations", h.listInvitations)
}

// ---------------------------------------------------------------------------
// DTOs
// ---------------------------------------------------------------------------

type Member struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	About       *string `json:"about"`
	AvatarURL   *string `json:"avatar_url"`
	Role        string  `json:"role"`
	Online      bool    `json:"online"`
	LastSeenAt  *string `json:"last_seen_at,omitempty"`
	JoinedAt    string  `json:"joined_at"`
	IsMe        bool    `json:"is_me"`
}

type LastMessage struct {
	ID        string `json:"id"`
	SenderID  string `json:"sender_id"`
	Sender    string `json:"sender_name"`
	Type      string `json:"type"`
	Preview   string `json:"preview"`
	Seq       int64  `json:"seq"`
	CreatedAt string `json:"created_at"`
}

type Conversation struct {
	ID            string       `json:"id"`
	Type          string       `json:"type"`
	Title         *string      `json:"title"`
	Description   *string      `json:"description"`
	AvatarURL     *string      `json:"avatar_url"`
	CreatedAt     string       `json:"created_at"`
	LastMessageAt *string      `json:"last_message_at,omitempty"`
	LastMessage   *LastMessage `json:"last_message,omitempty"`
	UnreadCount   int64        `json:"unread_count"`
	MemberCount   int          `json:"member_count"`
	MyRole        string       `json:"my_role"`
	IsMuted       bool         `json:"is_muted"`
	IsPinned      bool         `json:"is_pinned"`
	IsArchived    bool         `json:"is_archived"`
	ReadSeq       int64        `json:"read_seq"`
	OtherUser     *Member      `json:"other_user,omitempty"`
	Members       []Member     `json:"members,omitempty"`
}

// ---------------------------------------------------------------------------
// Direct conversations (duplicate-safe)
// ---------------------------------------------------------------------------

func (h *Handler) createDirect(w http.ResponseWriter, r *http.Request) {
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
			"You cannot open a conversation with yourself.", nil))
		return
	}
	ctx := r.Context()

	// Both users must be active; block relationships stop new conversations.
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
	var blocked bool
	if err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM blocked_users
			WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))
	`, me, target).Scan(&blocked); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if blocked {
		api.WriteError(w, r, api.ErrBlockedBy)
		return
	}

	a, b := me.String(), target.String()
	if a > b {
		a, b = b, a
	}
	pairKey := a + ":" + b

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(ctx)

	// Serialize concurrent get-or-create for the same pair.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, pairKey); err != nil {
		api.WriteError(w, r, err)
		return
	}

	var convID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT conversation_id FROM direct_conversation_keys WHERE pair_key = $1
	`, pairKey).Scan(&convID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Create conversation + memberships + sequences + settings atomically.
		err = tx.QueryRow(ctx, `
			INSERT INTO conversations (type, created_by) VALUES ('direct', $1) RETURNING id
		`, me).Scan(&convID)
		if err != nil {
			api.WriteError(w, r, err)
			return
		}
		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1,$2,'member'), ($1,$3,'member')`,
				[]any{convID, me, target}},
			{`INSERT INTO direct_conversation_keys (conversation_id, pair_key) VALUES ($1,$2)`,
				[]any{convID, pairKey}},
			{`INSERT INTO conversation_sequences (conversation_id) VALUES ($1)`, []any{convID}},
			{`INSERT INTO conversation_settings (conversation_id) VALUES ($1)`, []any{convID}},
			{`INSERT INTO message_read_cursors (conversation_id, user_id) VALUES ($1,$2), ($1,$3)`,
				[]any{convID, me, target}},
		} {
			if _, err := tx.Exec(ctx, stmt.sql, stmt.args...); err != nil {
				api.WriteError(w, r, err)
				return
			}
		}
	} else if err != nil {
		api.WriteError(w, r, err)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		api.WriteError(w, r, err)
		return
	}

	conv, err := h.loadConversation(r, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	// Let the other user know a conversation now exists (if they were offline
	// when it was created they will still discover it on next sync).
	ids, _ := h.memberIDs(ctx, convID, true)
	h.hub.ToUsers(ids, map[string]any{
		"type":         "conversation.created",
		"event_id":     uuid.NewString(),
		"conversation": conv,
	}, "")
	api.WriteJSON(w, r, http.StatusOK, conv)
}

// ---------------------------------------------------------------------------
// Groups
// ---------------------------------------------------------------------------

func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string   `json:"title"`
		Description *string  `json:"description"`
		MemberIDs   []string `json:"member_ids"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	title := strings.TrimSpace(req.Title)
	if len([]rune(title)) < 1 || len([]rune(title)) > 100 {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Group title is required (max 100 characters).", nil))
		return
	}
	me := api.UserID(r.Context())
	ctx := r.Context()

	members := []uuid.UUID{me}
	seen := map[uuid.UUID]bool{me: true}
	for _, raw := range req.MemberIDs {
		id, err := uuid.Parse(raw)
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		members = append(members, id)
	}
	if len(members) > 100 {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Groups are limited to 100 members.", nil))
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(ctx)

	var convID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO conversations (type, title, description, created_by)
		VALUES ('group', $1, NULLIF($2,''), $3)
		RETURNING id
	`, title, deref(req.Description), me).Scan(&convID); err != nil {
		api.WriteError(w, r, err)
		return
	}

	for _, m := range members {
		role := "member"
		if m == me {
			role = "owner"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_members (conversation_id, user_id, role)
			VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
		`, convID, m, role); err != nil {
			api.WriteError(w, r, err)
			return
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO message_read_cursors (conversation_id, user_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, convID, m); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}
	for _, stmt := range []string{
		`INSERT INTO conversation_sequences (conversation_id) VALUES ($1)`,
		`INSERT INTO conversation_settings (conversation_id) VALUES ($1)`,
	} {
		if _, err := tx.Exec(ctx, stmt, convID); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}

	// System message: group creation.
	var creatorName string
	if err := tx.QueryRow(ctx,
		`SELECT display_name FROM user_profiles WHERE user_id = $1`, me).Scan(&creatorName); err != nil {
		api.WriteError(w, r, err)
		return
	}
	sysID, err := insertSystemMessageTx(ctx, tx, convID, creatorName+" created the group \""+title+"\"")
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE conversations SET last_message_at = now() WHERE id = $1`, convID); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		api.WriteError(w, r, err)
		return
	}

	conv, err := h.loadConversation(r, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	ids, _ := h.memberIDs(ctx, convID, true)
	convJSON := map[string]any{
		"type":           "conversation.created",
		"event_id":       uuid.NewString(),
		"conversation":   conv,
		"system_message": map[string]any{"id": sysID},
	}
	h.hub.ToUsers(ids, convJSON, "")
	for _, m := range members {
		if m != me {
			h.notify.Notify(ctx, m, "group_invite", "Added to a group",
				creatorName+" added you to \""+title+"\".", &convID, nil, &me)
		}
	}
	api.WriteJSON(w, r, http.StatusCreated, conv)
}

// seqExec is satisfied by both pgx.Tx and *pgxpool.Pool.
type seqExec interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// insertSystemMessageTx allocates a sequence and stores a system message.
func insertSystemMessageTx(ctx context.Context, tx seqExec, conversationID uuid.UUID, text string) (uuid.UUID, error) {
	var seq int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO conversation_sequences (conversation_id, last_seq) VALUES ($1, 1)
		ON CONFLICT (conversation_id) DO UPDATE SET last_seq = conversation_sequences.last_seq + 1
		RETURNING last_seq
	`, conversationID).Scan(&seq); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO messages (conversation_id, type, text, seq)
		VALUES ($1, 'system', $2, $3) RETURNING id
	`, conversationID, text, seq).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// InsertSystemMessage is the exported helper used by other packages.
func (h *Handler) InsertSystemMessage(ctx context.Context, conversationID uuid.UUID, text string) (uuid.UUID, error) {
	return insertSystemMessageTx(ctx, h.pool, conversationID, text)
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	me := api.UserID(r.Context())
	filter := r.URL.Query().Get("filter")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 100

	// Load the member's conversations with a single projection query.
	rows, err := h.pool.Query(r.Context(), `
		SELECT conv_id, conv_type, conv_title, conv_description, conv_avatar, conv_created_at,
		       conv_last_message_at, my_role, my_muted, my_pinned, my_archived, my_read_seq, member_count
		FROM (
			SELECT c.id AS conv_id, c.type AS conv_type, c.title AS conv_title, c.description AS conv_description,
			       c.avatar_attachment_id AS conv_avatar, c.created_at AS conv_created_at,
			       c.last_message_at AS conv_last_message_at,
			       m.role AS my_role, m.is_muted AS my_muted, m.is_pinned AS my_pinned, m.is_archived AS my_archived,
			       cur.read_seq AS my_read_seq,
			       (SELECT count(*) FROM conversation_members cm2 WHERE cm2.conversation_id = c.id AND cm2.left_at IS NULL) AS member_count
			FROM conversation_members m
			JOIN conversations c ON c.id = m.conversation_id
			JOIN message_read_cursors cur ON cur.conversation_id = c.id AND cur.user_id = m.user_id
			WHERE m.user_id = $2 AND m.left_at IS NULL
		) x
		ORDER BY (my_pinned) DESC, conv_last_message_at DESC NULLS LAST
		LIMIT $1
	`, limit, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	_ = q // conversation-name search handled client-side on the loaded list (local scale)
	out := []Conversation{}
	for rows.Next() {
		var convID uuid.UUID
		var convType string
		var title, description *string
		var avatarID *uuid.UUID
		var createdAt time.Time
		var lastMessageAt *time.Time
		var myRole string
		var muted, pinned, archived bool
		var readSeq int64
		var memberCount int
		if err := rows.Scan(&convID, &convType, &title, &description, &avatarID, &createdAt,
			&lastMessageAt, &myRole, &muted, &pinned, &archived, &readSeq, &memberCount); err != nil {
			api.WriteError(w, r, err)
			return
		}

		conv, err := h.assembleConversation(r, convID, me, convType, title, description, avatarID,
			createdAt, lastMessageAt, myRole, muted, pinned, archived, readSeq, memberCount)
		if err != nil {
			continue
		}

		// Filters applied on the assembled view.
		switch filter {
		case "unread":
			if conv.UnreadCount == 0 {
				continue
			}
		case "pinned":
			if !conv.IsPinned {
				continue
			}
		case "archived":
			if !conv.IsArchived {
				continue
			}
		case "groups", "direct":
			if conv.Type != filter {
				continue
			}
		}
		if q != "" && !conversationMatches(conv, q) {
			continue
		}
		out = append(out, *conv)
	}
	api.WriteJSON(w, r, http.StatusOK, out)
}

func conversationMatches(c *Conversation, q string) bool {
	ql := strings.ToLower(q)
	if c.Title != nil && strings.Contains(strings.ToLower(*c.Title), ql) {
		return true
	}
	if c.OtherUser != nil &&
		(strings.Contains(strings.ToLower(c.OtherUser.DisplayName), ql) ||
			strings.Contains(strings.ToLower(c.OtherUser.Username), ql)) {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Details / update
// ---------------------------------------------------------------------------

func (h *Handler) details(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	if ok, err := h.isMember(r, convID, me); err != nil || !ok {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}
	conv, err := h.loadConversation(r, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, conv)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	ctx := r.Context()

	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}

	role, convType, err := h.myRole(ctx, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if convType != "group" {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Only group conversations can be edited.", nil))
		return
	}
	if role != "owner" && role != "admin" {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}

	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if len([]rune(title)) < 1 || len([]rune(title)) > 100 {
			api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
				"Group title is required (max 100 characters).", nil))
			return
		}
		if _, err := h.pool.Exec(ctx,
			`UPDATE conversations SET title = $1, updated_at = now() WHERE id = $2`, title, convID); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}
	if req.Description != nil {
		d := strings.TrimSpace(*req.Description)
		if len([]rune(d)) > 500 {
			api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
				"Description is limited to 500 characters.", nil))
			return
		}
		if _, err := h.pool.Exec(ctx,
			`UPDATE conversations SET description = NULLIF($1,''), updated_at = now() WHERE id = $2`, d, convID); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}

	conv, err := h.loadConversation(r, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	ids, _ := h.memberIDs(ctx, convID, true)
	h.hub.ToUsers(ids, map[string]any{
		"type":         "conversation.updated",
		"event_id":     uuid.NewString(),
		"conversation": conv,
	}, "")
	api.WriteJSON(w, r, http.StatusOK, conv)
}

func (h *Handler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	ctx := r.Context()

	role, convType, err := h.myRole(ctx, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	_ = role
	if convType != "group" {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}
	// Only the owner may delete a group.
	var isOwner bool
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=$1 AND user_id=$2 AND role='owner' AND left_at IS NULL)`,
		convID, me).Scan(&isOwner); err != nil || !isOwner {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}
	ids, _ := h.memberIDs(ctx, convID, true)
	if _, err := h.pool.Exec(ctx, `DELETE FROM conversations WHERE id = $1`, convID); err != nil {
		api.WriteError(w, r, err)
		return
	}
	h.hub.ToUsers(ids, map[string]any{
		"type":            "conversation.deleted",
		"event_id":        uuid.NewString(),
		"conversation_id": convID.String(),
	}, "")
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// ---------------------------------------------------------------------------
// Per-member settings
// ---------------------------------------------------------------------------

func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	var req struct {
		IsMuted           *bool   `json:"is_muted"`
		IsPinned          *bool   `json:"is_pinned"`
		IsArchived        *bool   `json:"is_archived"`
		NotificationLevel *string `json:"notification_level"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	ctx := r.Context()

	if req.NotificationLevel != nil {
		lvl := *req.NotificationLevel
		if lvl != "all" && lvl != "mentions_only" && lvl != "none" {
			api.WriteError(w, r, api.ErrValidation)
			return
		}
		if _, err := h.pool.Exec(ctx,
			`UPDATE conversation_members SET notification_level = $1 WHERE conversation_id = $2 AND user_id = $3`,
			lvl, convID, me); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}
	for _, pair := range []struct {
		col string
		val *bool
	}{
		{"is_muted", req.IsMuted},
		{"is_pinned", req.IsPinned},
		{"is_archived", req.IsArchived},
	} {
		if pair.val == nil {
			continue
		}
		if _, err := h.pool.Exec(ctx,
			`UPDATE conversation_members SET `+pair.col+` = $1 WHERE conversation_id = $2 AND user_id = $3`,
			*pair.val, convID, me); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}

	conv, err := h.loadConversation(r, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, conv)
}

// ---------------------------------------------------------------------------
// Membership
// ---------------------------------------------------------------------------

func (h *Handler) addMembers(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	var req struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	ctx := r.Context()

	role, convType, err := h.myRole(ctx, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if convType != "group" {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Direct conversations have fixed membership.", nil))
		return
	}
	var canInvite bool
	if err := h.pool.QueryRow(ctx,
		`SELECT members_can_invite FROM conversation_settings WHERE conversation_id = $1`, convID).Scan(&canInvite); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if role != "owner" && role != "admin" && !canInvite {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}

	var actorName string
	_ = h.pool.QueryRow(ctx, `SELECT display_name FROM user_profiles WHERE user_id = $1`, me).Scan(&actorName)

	added := []uuid.UUID{}
	for _, raw := range req.UserIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		var status string
		if err := h.pool.QueryRow(ctx, `SELECT account_status FROM users WHERE id = $1`, id).Scan(&status); err != nil || status != "active" {
			continue
		}
		tag, err := h.pool.Exec(ctx, `
			INSERT INTO conversation_members (conversation_id, user_id, role)
			VALUES ($1, $2, 'member')
			ON CONFLICT (conversation_id, user_id) DO UPDATE SET left_at = NULL, joined_at = now()
		`, convID, id)
		if err != nil {
			continue
		}
		if tag.RowsAffected() > 0 {
			added = append(added, id)
		}
		if _, err := h.pool.Exec(ctx, `
			INSERT INTO message_read_cursors (conversation_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
		`, convID, id); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}

	for _, m := range added {
		var name string
		_ = h.pool.QueryRow(ctx, `SELECT display_name FROM user_profiles WHERE user_id = $1`, m).Scan(&name)
		if _, err := h.InsertSystemMessage(ctx, convID, actorName+" added "+name); err != nil {
			api.WriteError(w, r, err)
			return
		}
		h.notify.Notify(ctx, m, "group_invite", "Added to a group",
			actorName+" added you to a group.", &convID, nil, &me)
	}
	if _, err := h.pool.Exec(ctx,
		`UPDATE conversations SET last_message_at = now(), updated_at = now() WHERE id = $1`, convID); err != nil {
		api.WriteError(w, r, err)
		return
	}

	conv, err := h.loadConversation(r, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	ids, _ := h.memberIDs(ctx, convID, true)
	h.hub.ToUsers(ids, map[string]any{
		"type":           "conversation.member_added",
		"event_id":       uuid.NewString(),
		"conversation":   conv,
		"added_user_ids": uuidStrings(added),
	}, "")
	api.WriteJSON(w, r, http.StatusOK, conv)
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	target, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	ctx := r.Context()

	role, convType, err := h.myRole(ctx, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if convType != "group" {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}
	if role != "owner" && role != "admin" {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}
	// Admins cannot remove owners.
	var targetRole string
	if err := h.pool.QueryRow(ctx, `
		SELECT role FROM conversation_members WHERE conversation_id = $1 AND user_id = $2 AND left_at IS NULL
	`, convID, target).Scan(&targetRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.WriteError(w, r, api.ErrNotFound)
			return
		}
		api.WriteError(w, r, err)
		return
	}
	if targetRole == "owner" {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}

	var actorName, targetName string
	_ = h.pool.QueryRow(ctx, `SELECT display_name FROM user_profiles WHERE user_id = $1`, me).Scan(&actorName)
	_ = h.pool.QueryRow(ctx, `SELECT display_name FROM user_profiles WHERE user_id = $1`, target).Scan(&targetName)

	if _, err := h.pool.Exec(ctx,
		`UPDATE conversation_members SET left_at = now() WHERE conversation_id = $1 AND user_id = $2`, convID, target); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if _, err := h.InsertSystemMessage(ctx, convID, actorName+" removed "+targetName); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if _, err := h.pool.Exec(ctx,
		`UPDATE conversations SET last_message_at = now(), updated_at = now() WHERE id = $1`, convID); err != nil {
		api.WriteError(w, r, err)
		return
	}

	ids, _ := h.memberIDs(ctx, convID, true)
	h.hub.ToUsers(ids, map[string]any{
		"type":            "conversation.member_removed",
		"event_id":        uuid.NewString(),
		"conversation_id": convID.String(),
		"removed_user_id": target.String(),
	}, "")
	// The removed member is no longer in memberIDs; notify them directly.
	h.hub.ToUsers([]uuid.UUID{target}, map[string]any{
		"type":            "conversation.member_removed",
		"event_id":        uuid.NewString(),
		"conversation_id": convID.String(),
		"removed_user_id": target.String(),
	}, "")
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"removed": true})
}

func (h *Handler) changeRole(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	target, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	var req struct {
		Role string `json:"role"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if req.Role != "admin" && req.Role != "member" {
		api.WriteError(w, r, api.ErrValidation)
		return
	}
	ctx := r.Context()

	role, _, err := h.myRole(ctx, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if role != "owner" {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}
	tag, err := h.pool.Exec(ctx, `
		UPDATE conversation_members SET role = $1
		WHERE conversation_id = $2 AND user_id = $3 AND left_at IS NULL AND role <> 'owner'
	`, req.Role, convID, target)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"role": req.Role})
}

func (h *Handler) leave(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	ctx := r.Context()

	role, convType, err := h.myRole(ctx, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if convType != "group" {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Direct conversations cannot be left.", nil))
		return
	}

	var myName string
	_ = h.pool.QueryRow(ctx, `SELECT display_name FROM user_profiles WHERE user_id = $1`, me).Scan(&myName)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`UPDATE conversation_members SET left_at = now() WHERE conversation_id = $1 AND user_id = $2`, convID, me); err != nil {
		api.WriteError(w, r, err)
		return
	}

	// Owner leaving: transfer ownership to the earliest-joined remaining member.
	if role == "owner" {
		var successor *uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT user_id FROM conversation_members
			WHERE conversation_id = $1 AND left_at IS NULL AND user_id <> $2
			ORDER BY joined_at LIMIT 1
		`, convID, me).Scan(&successor); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			api.WriteError(w, r, err)
			return
		}
		if successor != nil {
			if _, err := tx.Exec(ctx,
				`UPDATE conversation_members SET role = 'owner' WHERE conversation_id = $1 AND user_id = $2`,
				convID, *successor); err != nil {
				api.WriteError(w, r, err)
				return
			}
			var succName string
			_ = tx.QueryRow(ctx, `SELECT display_name FROM user_profiles WHERE user_id = $1`, *successor).Scan(&succName)
			if _, err := insertSystemMessageTx(ctx, tx, convID, succName+" is now the group owner"); err != nil {
				api.WriteError(w, r, err)
				return
			}
		} else {
			// Last member: remove the group entirely.
			if err := tx.Commit(ctx); err != nil {
				api.WriteError(w, r, err)
				return
			}
			if _, err := h.pool.Exec(ctx, `DELETE FROM conversations WHERE id = $1`, convID); err != nil {
				api.WriteError(w, r, err)
				return
			}
			api.WriteJSON(w, r, http.StatusOK, map[string]any{"left": true, "group_deleted": true})
			return
		}
	}

	if _, err := insertSystemMessageTx(ctx, tx, convID, myName+" left the group"); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE conversations SET last_message_at = now(), updated_at = now() WHERE id = $1`, convID); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		api.WriteError(w, r, err)
		return
	}

	ids, _ := h.memberIDs(ctx, convID, true)
	h.hub.ToUsers(ids, map[string]any{
		"type":            "conversation.member_removed",
		"event_id":        uuid.NewString(),
		"conversation_id": convID.String(),
		"removed_user_id": me.String(),
	}, "")
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"left": true})
}

// ---------------------------------------------------------------------------
// Invitations
// ---------------------------------------------------------------------------

func (h *Handler) invite(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	var req struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	ctx := r.Context()

	role, convType, err := h.myRole(ctx, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if convType != "group" {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Only groups can have invitations.", nil))
		return
	}
	var canInvite bool
	if err := h.pool.QueryRow(ctx,
		`SELECT members_can_invite FROM conversation_settings WHERE conversation_id = $1`, convID).Scan(&canInvite); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if role != "owner" && role != "admin" && !canInvite {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}

	created := []uuid.UUID{}
	for _, raw := range req.UserIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		var status string
		if err := h.pool.QueryRow(ctx, `SELECT account_status FROM users WHERE id = $1`, id).Scan(&status); err != nil || status != "active" {
			continue
		}
		var invID uuid.UUID
		if err := h.pool.QueryRow(ctx, `
			INSERT INTO group_invitations (conversation_id, inviter_id, invitee_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (conversation_id, invitee_id) DO UPDATE SET status = 'pending', created_at = now(), responded_at = NULL
			RETURNING id
		`, convID, me, id).Scan(&invID); err == nil {
			created = append(created, id)
			var title string
			_ = h.pool.QueryRow(ctx, `SELECT title FROM conversations WHERE id = $1`, convID).Scan(&title)
			h.notify.Notify(ctx, id, "group_invite", "Group invitation",
				"You have been invited to \""+title+"\".", &convID, nil, &me)
		}
	}
	api.WriteJSON(w, r, http.StatusCreated, map[string]any{"invited": uuidStrings(created)})
}

func (h *Handler) listInvitations(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	if ok, err := h.isMember(r, convID, me); err != nil || !ok {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT i.id, i.invitee_id, u.username, p.display_name, p.avatar_attachment_id, i.status, i.created_at
		FROM group_invitations i
		JOIN users u ON u.id = i.invitee_id
		JOIN user_profiles p ON p.user_id = i.invitee_id
		WHERE i.conversation_id = $1 AND i.status = 'pending'
		ORDER BY i.created_at DESC
	`, convID)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	type inv struct {
		ID          string  `json:"id"`
		UserID      string  `json:"user_id"`
		Username    string  `json:"username"`
		DisplayName string  `json:"display_name"`
		AvatarURL   *string `json:"avatar_url"`
		Status      string  `json:"status"`
		CreatedAt   string  `json:"created_at"`
	}
	out := []inv{}
	for rows.Next() {
		var it inv
		var id, uid uuid.UUID
		var avatarID *uuid.UUID
		var createdAt time.Time
		if err := rows.Scan(&id, &uid, &it.Username, &it.DisplayName, &avatarID, &it.Status, &createdAt); err != nil {
			api.WriteError(w, r, err)
			return
		}
		it.ID = id.String()
		it.UserID = uid.String()
		if avatarID != nil {
			url := "/v1/attachments/" + avatarID.String() + "/download"
			it.AvatarURL = &url
		}
		it.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		out = append(out, it)
	}
	api.WriteJSON(w, r, http.StatusOK, out)
}

func (h *Handler) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	h.respondToInvitation(w, r, true)
}

func (h *Handler) declineInvitation(w http.ResponseWriter, r *http.Request) {
	h.respondToInvitation(w, r, false)
}

func (h *Handler) respondToInvitation(w http.ResponseWriter, r *http.Request, accept bool) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	invID, err := uuid.Parse(chi.URLParam(r, "invitationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	ctx := r.Context()

	status := "declined"
	if accept {
		status = "accepted"
	}
	tag, err := h.pool.Exec(ctx, `
		UPDATE group_invitations SET status = $1, responded_at = now()
		WHERE id = $2 AND invitee_id = $3 AND status = 'pending'
	`, status, invID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		api.WriteError(w, r, api.ErrNotFound)
		return
	}

	if accept {
		if _, err := h.pool.Exec(ctx, `
			INSERT INTO conversation_members (conversation_id, user_id, role)
			VALUES ($1, $2, 'member') ON CONFLICT DO NOTHING
		`, convID, me); err != nil {
			api.WriteError(w, r, err)
			return
		}
		if _, err := h.pool.Exec(ctx, `
			INSERT INTO message_read_cursors (conversation_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
		`, convID, me); err != nil {
			api.WriteError(w, r, err)
			return
		}
		var myName string
		_ = h.pool.QueryRow(ctx, `SELECT display_name FROM user_profiles WHERE user_id = $1`, me).Scan(&myName)
		if _, err := h.InsertSystemMessage(ctx, convID, myName+" joined the group"); err != nil {
			api.WriteError(w, r, err)
			return
		}
		if _, err := h.pool.Exec(ctx,
			`UPDATE conversations SET last_message_at = now() WHERE id = $1`, convID); err != nil {
			api.WriteError(w, r, err)
			return
		}
		conv, err := h.loadConversation(r, convID, me)
		if err != nil {
			api.WriteError(w, r, err)
			return
		}
		ids, _ := h.memberIDs(ctx, convID, true)
		h.hub.ToUsers(ids, map[string]any{
			"type":           "conversation.member_added",
			"event_id":       uuid.NewString(),
			"conversation":   conv,
			"added_user_ids": []string{me.String()},
		}, "")
		api.WriteJSON(w, r, http.StatusOK, conv)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"status": "declined"})
}

// ---------------------------------------------------------------------------
// Shared internals
// ---------------------------------------------------------------------------

func (h *Handler) isMember(r *http.Request, convID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := h.pool.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=$1 AND user_id=$2 AND left_at IS NULL)
	`, convID, userID).Scan(&ok)
	return ok, err
}

// IsMember is exported for other packages (messages, receipts, search).
func (h *Handler) IsMember(ctx context.Context, convID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=$1 AND user_id=$2 AND left_at IS NULL)
	`, convID, userID).Scan(&ok)
	return ok, err
}

// MyRole is exported for other packages (attachments avatars).
func (h *Handler) MyRole(ctx context.Context, convID, userID uuid.UUID) (string, string, error) {
	return h.myRole(ctx, convID, userID)
}

func (h *Handler) myRole(ctx context.Context, convID, userID uuid.UUID) (string, string, error) {
	var role, convType string
	err := h.pool.QueryRow(ctx, `
		SELECT m.role, c.type FROM conversation_members m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.conversation_id = $1 AND m.user_id = $2 AND m.left_at IS NULL
	`, convID, userID).Scan(&role, &convType)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", api.ErrForbidden
	}
	return role, convType, err
}

func (h *Handler) memberIDs(ctx context.Context, convID uuid.UUID, activeOnly bool) ([]uuid.UUID, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT user_id FROM conversation_members WHERE conversation_id = $1 AND left_at IS NULL
	`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// MemberIDs is exported for the realtime layer.
func (h *Handler) MemberIDs(ctx context.Context, convID uuid.UUID) ([]uuid.UUID, error) {
	return h.memberIDs(ctx, convID, true)
}

func (h *Handler) loadConversation(r *http.Request, convID, me uuid.UUID) (*Conversation, error) {
	var convType string
	var title, description *string
	var avatarID *uuid.UUID
	var createdAt time.Time
	var lastMessageAt *time.Time
	var myRole string
	var muted, pinned, archived bool
	var readSeq int64
	var memberCount int
	err := h.pool.QueryRow(r.Context(), `
		SELECT c.type, c.title, c.description, c.avatar_attachment_id, c.created_at, c.last_message_at,
		       m.role, m.is_muted, m.is_pinned, m.is_archived, cur.read_seq,
		       (SELECT count(*) FROM conversation_members cm2 WHERE cm2.conversation_id = c.id AND cm2.left_at IS NULL)
		FROM conversation_members m
		JOIN conversations c ON c.id = m.conversation_id
		JOIN message_read_cursors cur ON cur.conversation_id = c.id AND cur.user_id = m.user_id
		WHERE c.id = $1 AND m.user_id = $2 AND m.left_at IS NULL
	`, convID, me).Scan(&convType, &title, &description, &avatarID, &createdAt, &lastMessageAt,
		&myRole, &muted, &pinned, &archived, &readSeq, &memberCount)
	if err != nil {
		return nil, err
	}
	return h.assembleConversation(r, convID, me, convType, title, description, avatarID, createdAt,
		lastMessageAt, myRole, muted, pinned, archived, readSeq, memberCount)
}

//nolint:gocritic // long projection list is inherent to the domain
func (h *Handler) assembleConversation(r *http.Request, convID, me uuid.UUID, convType string,
	title, description *string, avatarID *uuid.UUID, createdAt time.Time, lastMessageAt *time.Time,
	myRole string, muted, pinned, archived bool, readSeq int64, memberCount int) (*Conversation, error) {

	ctx := r.Context()
	conv := &Conversation{
		ID:          convID.String(),
		Type:        convType,
		Title:       title,
		Description: description,
		MyRole:      myRole,
		IsMuted:     muted,
		IsPinned:    pinned,
		IsArchived:  archived,
		ReadSeq:     readSeq,
		MemberCount: memberCount,
	}
	conv.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	if avatarID != nil {
		url := "/v1/attachments/" + avatarID.String() + "/download"
		conv.AvatarURL = &url
	}
	if lastMessageAt != nil {
		ts := lastMessageAt.UTC().Format(time.RFC3339)
		conv.LastMessageAt = &ts
	}

	// Load members with presence.
	rows, err := h.pool.Query(ctx, `
		SELECT m.user_id, u.username, p.display_name, p.about, p.avatar_attachment_id, m.role, m.joined_at,
		       u.last_seen_at, s.online_visibility, s.last_seen_visibility
		FROM conversation_members m
		JOIN users u ON u.id = m.user_id
		JOIN user_profiles p ON p.user_id = m.user_id
		JOIN user_privacy_settings s ON s.user_id = m.user_id
		WHERE m.conversation_id = $1 AND m.left_at IS NULL
		ORDER BY m.joined_at
	`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type memberRow struct {
		id          uuid.UUID
		username    string
		display     string
		about       *string
		avatarID    *uuid.UUID
		role        string
		joinedAt    time.Time
		lastSeen    *time.Time
		onlineVis   string
		lastSeenVis string
	}
	var members []memberRow
	for rows.Next() {
		var m memberRow
		if err := rows.Scan(&m.id, &m.username, &m.display, &m.about, &m.avatarID, &m.role, &m.joinedAt,
			&m.lastSeen, &m.onlineVis, &m.lastSeenVis); err != nil {
			return nil, err
		}
		members = append(members, m)
	}

	for _, m := range members {
		member := Member{
			ID:          m.id.String(),
			Username:    m.username,
			DisplayName: m.display,
			About:       m.about,
			Role:        m.role,
			JoinedAt:    m.joinedAt.UTC().Format(time.RFC3339),
			IsMe:        m.id == me,
		}
		if m.avatarID != nil {
			url := "/v1/attachments/" + m.avatarID.String() + "/download"
			member.AvatarURL = &url
		}
		// Presence between co-members: sharing a conversation establishes
		// context; the target's settings still govern visibility.
		canSeePresence := m.onlineVis == "everyone" ||
			(m.onlineVis == "contacts" && h.areContacts(ctx, me, m.id)) ||
			m.id == me
		if canSeePresence {
			member.Online = h.presence.IsOnline(ctx, m.id)
		}
		if m.lastSeen != nil && canSeePresence && m.onlineVis != "nobody" {
			ts := m.lastSeen.UTC().Format(time.RFC3339)
			member.LastSeenAt = &ts
		}
		conv.Members = append(conv.Members, member)
		if convType == "direct" && m.id != me {
			other := member
			conv.OtherUser = &other
			if conv.Title == nil {
				t := other.DisplayName
				conv.Title = &t
			}
		}
	}

	// Unread count from my cursor.
	var unread int64
	_ = h.pool.QueryRow(ctx, `
		SELECT count(*) FROM messages
		WHERE conversation_id = $1 AND seq > $2 AND deleted_at IS NULL AND sender_id IS DISTINCT FROM $3 AND type <> 'system'
	`, convID, readSeq, me).Scan(&unread)
	conv.UnreadCount = unread

	// Last message preview.
	var lm LastMessage
	var lmID, lmSender *uuid.UUID
	var lmType string
	var lmText *string
	var lmSeq int64
	var lmCreatedAt time.Time
	var lmSenderName *string
	err = h.pool.QueryRow(ctx, `
		SELECT m.id, m.sender_id, m.type, m.text, m.seq, m.created_at, p.display_name
		FROM messages m
		LEFT JOIN user_profiles p ON p.user_id = m.sender_id
		WHERE m.conversation_id = $1 AND m.deleted_at IS NULL
		ORDER BY m.seq DESC LIMIT 1
	`, convID).Scan(&lmID, &lmSender, &lmType, &lmText, &lmSeq, &lmCreatedAt, &lmSenderName)
	if err == nil {
		lm.ID = lmID.String()
		lm.Type = lmType
		lm.Seq = lmSeq
		lm.CreatedAt = lmCreatedAt.UTC().Format(time.RFC3339)
		if lmSender != nil {
			lm.SenderID = lmSender.String()
		}
		lm.Sender = deref(lmSenderName)
		lm.Preview = previewFor(lmType, lmText)
		conv.LastMessage = &lm
	}
	return conv, nil
}

func (h *Handler) areContacts(ctx context.Context, a, b uuid.UUID) bool {
	var ok bool
	err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM contacts WHERE owner_id=$1 AND contact_id=$2) OR
		       EXISTS(SELECT 1 FROM contacts WHERE owner_id=$2 AND contact_id=$1)
	`, a, b).Scan(&ok)
	return err == nil && ok
}

func previewFor(msgType string, text *string) string {
	switch msgType {
	case "system":
		return deref(text)
	case "image":
		return "📷 Photo"
	case "video":
		return "🎥 Video"
	case "audio", "voice":
		return "🎙 Voice message"
	case "document":
		return "📄 Document"
	}
	return deref(text)
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
