// Package search implements message search (full-text + conversation
// filtering). Every query is membership-authorized; results never leak
// conversations the requester cannot access.
package search

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singhtanishq/veylo/services/api/internal/api"
)

// Handler exposes search endpoints.
type Handler struct {
	pool *pgxpool.Pool
}

// NewHandler constructs the search handler.
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

// Routes registers search endpoints (auth applied by router).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/search/messages", h.messages)
	r.Get("/search/conversations", h.conversations)
}

type messageHit struct {
	ID             string  `json:"id"`
	ConversationID string  `json:"conversation_id"`
	SenderID       string  `json:"sender_id"`
	SenderName     string  `json:"sender_name"`
	Type           string  `json:"type"`
	Text           string  `json:"text"`
	Seq            int64   `json:"seq"`
	SentAt         string  `json:"sent_at"`
	Headline       *string `json:"headline,omitempty"`
}

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	me := api.UserID(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 1 || len(q) > 200 {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Search needs 1-200 characters.", nil))
		return
	}
	convFilter := r.URL.Query().Get("conversation_id")
	limit := 50
	ctx := r.Context()

	// Membership authorization scope:
	//  - conversation_id given: must be a member of THAT conversation;
	//  - otherwise: restrict to all of the requester's conversations.
	if convFilter != "" {
		convID, err := uuid.Parse(convFilter)
		if err != nil {
			api.WriteError(w, r, api.ErrBadRequest)
			return
		}
		var member bool
		if err := h.pool.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=$1 AND user_id=$2 AND left_at IS NULL)
		`, convID, me).Scan(&member); err != nil || !member {
			api.WriteError(w, r, api.ErrForbidden)
			return
		}
	}

	rows, err := h.pool.Query(ctx, `
		SELECT m.id, m.conversation_id, m.sender_id, COALESCE(p.display_name,''), m.type, m.text, m.seq, m.created_at,
		       ts_headline('simple', m.text, websearch_to_tsquery('simple', $1))
		FROM messages m
		JOIN user_profiles p ON p.user_id = m.sender_id
		JOIN conversation_members cm ON cm.conversation_id = m.conversation_id AND cm.user_id = $2 AND cm.left_at IS NULL
		WHERE m.deleted_at IS NULL AND m.type = 'text'
		  AND to_tsvector('simple', m.text) @@ websearch_to_tsquery('simple', $1)
		  AND ($3 = '' OR m.conversation_id::text = $3)
		ORDER BY ts_rank(to_tsvector('simple', m.text), websearch_to_tsquery('simple', $1)) DESC, m.seq DESC
		LIMIT $4
	`, q, me, convFilter, limit)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	out := []messageHit{}
	for rows.Next() {
		var hit messageHit
		var id, convID, senderID uuid.UUID
		var createdAt time.Time
		if err := rows.Scan(&id, &convID, &senderID, &hit.SenderName, &hit.Type, &hit.Text, &hit.Seq,
			&createdAt, &hit.Headline); err != nil {
			api.WriteError(w, r, err)
			return
		}
		hit.ID = id.String()
		hit.ConversationID = convID.String()
		hit.SenderID = senderID.String()
		hit.SentAt = createdAt.UTC().Format(time.RFC3339)
		out = append(out, hit)
	}
	api.WriteJSON(w, r, http.StatusOK, out)
}

func (h *Handler) conversations(w http.ResponseWriter, r *http.Request) {
	me := api.UserID(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 1 || len(q) > 100 {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Search needs 1-100 characters.", nil))
		return
	}
	pat := "%" + strings.ToLower(q) + "%"
	rows, err := h.pool.Query(r.Context(), `
		SELECT DISTINCT c.id, c.type, c.title, c.avatar_attachment_id, c.last_message_at
		FROM conversations c
		JOIN conversation_members m ON m.conversation_id = c.id AND m.user_id = $1 AND m.left_at IS NULL
		LEFT JOIN conversation_members m2 ON m2.conversation_id = c.id AND m2.left_at IS NULL AND m2.user_id <> $1
		LEFT JOIN user_profiles p2 ON p2.user_id = m2.user_id
		WHERE (c.type = 'group' AND c.title ILIKE $2)
		   OR (c.type = 'direct' AND p2.display_name ILIKE $2)
		   OR p2.display_name ILIKE $2
		LIMIT 20
	`, me, pat)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	type convHit struct {
		ID          string  `json:"id"`
		Type        string  `json:"type"`
		Title       *string `json:"title"`
		AvatarURL   *string `json:"avatar_url,omitempty"`
		LastMessage *string `json:"last_message_at,omitempty"`
	}
	out := []convHit{}
	for rows.Next() {
		var hit convHit
		var id uuid.UUID
		var avatarID *uuid.UUID
		var lastMsg *time.Time
		if err := rows.Scan(&id, &hit.Type, &hit.Title, &avatarID, &lastMsg); err != nil {
			api.WriteError(w, r, err)
			return
		}
		hit.ID = id.String()
		if avatarID != nil {
			url := "/v1/attachments/" + avatarID.String() + "/download"
			hit.AvatarURL = &url
		}
		if lastMsg != nil {
			ts := lastMsg.UTC().Format(time.RFC3339)
			hit.LastMessage = &ts
		}
		out = append(out, hit)
	}
	api.WriteJSON(w, r, http.StatusOK, out)
}
