// Package messages implements persistent, server-ordered messaging: sequence
// allocation, idempotent sends, cursor pagination, edits, deletes, replies,
// reactions and attachment wiring.
package messages

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singhtanishq/veylo/services/api/internal/api"
	"github.com/singhtanishq/veylo/services/api/internal/conversations"
	"github.com/singhtanishq/veylo/services/api/internal/notifications"
	"github.com/singhtanishq/veylo/services/api/internal/platform/ratelimit"
)

// Hub is the realtime fanout surface for messages.
type Hub interface {
	ToUsers(userIDs []uuid.UUID, payload any, excludeConn string)
}

var mentionRe = regexp.MustCompile(`@([a-z0-9_]{3,24})`)

// Handler exposes message endpoints.
type Handler struct {
	pool   *pgxpool.Pool
	conv   *conversations.Handler
	notify *notifications.Notifier
	hub    Hub
	limits *ratelimit.Limiter
}

// NewHandler constructs the messages handler.
func NewHandler(pool *pgxpool.Pool, conv *conversations.Handler, n *notifications.Notifier, hub Hub, l *ratelimit.Limiter) *Handler {
	return &Handler{pool: pool, conv: conv, notify: n, hub: hub, limits: l}
}

// SetHub wires the realtime hub after construction.
func (h *Handler) SetHub(hub Hub) { h.hub = hub }

// Routes registers message endpoints (auth applied by router).
func (h *Handler) Routes(r chi.Router) {
	r.Post("/conversations/{conversationID}/messages", h.send)
	r.Get("/conversations/{conversationID}/messages", h.list)
	r.Get("/conversations/{conversationID}/gallery", h.gallery)
	r.Patch("/messages/{messageID}", h.edit)
	r.Delete("/messages/{messageID}", h.delete)
	r.Put("/messages/{messageID}/reactions", h.addReaction)
	r.Delete("/messages/{messageID}/reactions", h.removeReaction)
	r.Get("/messages/{messageID}/receipts", h.receipts)
}

// ---------------------------------------------------------------------------
// DTOs
// ---------------------------------------------------------------------------

type Sender struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
}

type AttachmentMeta struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	MimeType     string  `json:"mime_type"`
	Kind         string  `json:"kind"`
	SizeBytes    int64   `json:"size_bytes"`
	Width        *int    `json:"width,omitempty"`
	Height       *int    `json:"height,omitempty"`
	DurationMS   *int    `json:"duration_ms,omitempty"`
	DownloadURL  string  `json:"download_url"`
	ThumbnailURL *string `json:"thumbnail_url,omitempty"`
}

type Reaction struct {
	Emoji   string   `json:"emoji"`
	Count   int      `json:"count"`
	UserIDs []string `json:"user_ids"`
	Mine    bool     `json:"mine"`
}

type Receipts struct {
	DeliveredCount int   `json:"delivered_count"`
	ReadCount      int   `json:"read_count"`
	Total          int   `json:"total"`
	MyReadSeq      int64 `json:"-"`
}

type Message struct {
	ID              string           `json:"id"`
	ConversationID  string           `json:"conversation_id"`
	Sender          *Sender          `json:"sender,omitempty"` // nil for system
	Type            string           `json:"type"`
	Text            *string          `json:"text,omitempty"`
	Seq             int64            `json:"seq"`
	ReplyTo         *ReplyPreview    `json:"reply_to,omitempty"`
	Attachments     []AttachmentMeta `json:"attachments,omitempty"`
	Reactions       []Reaction       `json:"reactions,omitempty"`
	CreatedAt       string           `json:"created_at"`
	EditedAt        *string          `json:"edited_at,omitempty"`
	Deleted         bool             `json:"deleted"`
	IsMine          bool             `json:"is_mine"`
	Receipts        *Receipts        `json:"receipts,omitempty"`
	ClientMessageID string           `json:"client_message_id,omitempty"`
}

type ReplyPreview struct {
	ID         string `json:"id"`
	SenderName string `json:"sender_name"`
	Type       string `json:"type"`
	Preview    string `json:"preview"`
}

// ---------------------------------------------------------------------------
// Send
// ---------------------------------------------------------------------------

// SendRequest is the shared send payload for REST and WebSocket paths.
type SendRequest struct {
	ConversationID  string   `json:"conversation_id,omitempty"` // set on the WS path
	Type            string   `json:"type"`
	Text            string   `json:"text"`
	ReplyToID       string   `json:"reply_to_id"`
	AttachmentIDs   []string `json:"attachment_ids"`
	ClientMessageID string   `json:"client_message_id"`
}

// Send persists a message transactionally and fans it out. Shared by the REST
// endpoint and the WebSocket message.send handler.
func (h *Handler) Send(ctx context.Context, conversationID, senderID uuid.UUID, req SendRequest, senderConn string) (*Message, error) {
	// Rate limit per user.
	if !h.limits.Allow("msg:"+senderID.String(), ratelimit.PolicyMessageWS.PerSec, ratelimit.PolicyMessageWS.Burst) {
		return nil, api.ErrRateLimited
	}

	// Membership authorization.
	ok, err := h.conv.IsMember(ctx, conversationID, senderID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, api.ErrForbidden
	}

	msgType := req.Type
	if msgType == "" {
		msgType = "text"
	}
	switch msgType {
	case "text", "image", "video", "audio", "voice", "document":
	default:
		return nil, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation, "Unsupported message type.", nil)
	}

	text := strings.TrimSpace(req.Text)
	if utf8.RuneCountInString(text) > 8000 {
		return nil, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation, "Messages are limited to 8,000 characters.", nil)
	}
	if msgType == "text" && text == "" {
		return nil, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation, "Message text cannot be empty.", nil)
	}

	// Attachment validation: all must exist, belong to the sender and not be
	// attached to another message yet.
	var attachmentIDs []uuid.UUID
	for _, raw := range req.AttachmentIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, api.ErrBadRequest
		}
		attachmentIDs = append(attachmentIDs, id)
	}
	if len(attachmentIDs) > 10 {
		return nil, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation, "A message can carry at most 10 attachments.", nil)
	}

	// Reply target must live in the same conversation.
	var replyTo *uuid.UUID
	if req.ReplyToID != "" {
		id, err := uuid.Parse(req.ReplyToID)
		if err != nil {
			return nil, api.ErrBadRequest
		}
		var inConv bool
		if err := h.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM messages WHERE id=$1 AND conversation_id=$2 AND deleted_at IS NULL)`,
			id, conversationID).Scan(&inConv); err != nil || !inConv {
			return nil, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation, "Reply target not found in this conversation.", nil)
		}
		replyTo = &id
	}

	var clientMsgID *uuid.UUID
	if req.ClientMessageID != "" {
		id, err := uuid.Parse(req.ClientMessageID)
		if err != nil {
			return nil, api.ErrBadRequest
		}
		clientMsgID = &id
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Idempotency: a duplicate client_message_id resolves to the original.
	if clientMsgID != nil {
		var existing uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT id FROM messages
			WHERE conversation_id=$1 AND sender_id=$2 AND client_message_id=$3
		`, conversationID, senderID, *clientMsgID).Scan(&existing)
		if err == nil {
			tx.Rollback(ctx)
			return h.Load(ctx, existing, senderID)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}

	// Authoritative per-conversation sequence allocation.
	var seq int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO conversation_sequences (conversation_id, last_seq) VALUES ($1, 1)
		ON CONFLICT (conversation_id) DO UPDATE SET last_seq = conversation_sequences.last_seq + 1
		RETURNING last_seq
	`, conversationID).Scan(&seq); err != nil {
		return nil, err
	}

	var msgID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO messages (conversation_id, sender_id, type, text, reply_to_id, seq, client_message_id)
		VALUES ($1, $2, $3, NULLIF($4,''), $5, $6, $7)
		RETURNING id
	`, conversationID, senderID, msgType, text, replyTo, seq, clientMsgID).Scan(&msgID); err != nil {
		return nil, err
	}

	// Wire attachments (validating ownership + uniqueness inside the tx).
	for i, attID := range attachmentIDs {
		var owned bool
		var kind string
		err := tx.QueryRow(ctx, `
			SELECT (owner_id = $2), kind FROM attachments WHERE id = $1
		`, attID, senderID).Scan(&owned, &kind)
		if err != nil || !owned {
			return nil, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
				"Attachment not found or not owned by you.", nil)
		}
		var used bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM message_attachments WHERE attachment_id = $1)`, attID).Scan(&used); err != nil {
			return nil, err
		}
		if used {
			return nil, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
				"Attachment was already used in another message.", nil)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO message_attachments (message_id, attachment_id, position) VALUES ($1, $2, $3)
		`, msgID, attID, i); err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE conversations SET last_message_at = now(), updated_at = now() WHERE id = $1`, conversationID); err != nil {
		return nil, err
	}

	// Advance the sender's own read + delivered cursors to this message.
	if _, err := tx.Exec(ctx, `
		INSERT INTO message_read_cursors (conversation_id, user_id, delivered_seq, read_seq)
		VALUES ($1, $2, $3, $3)
		ON CONFLICT (conversation_id, user_id) DO UPDATE
		SET delivered_seq = GREATEST(message_read_cursors.delivered_seq, EXCLUDED.delivered_seq),
		    read_seq = GREATEST(message_read_cursors.read_seq, EXCLUDED.read_seq),
		    updated_at = now()
	`, conversationID, senderID, seq); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	msg, err := h.Load(ctx, msgID, senderID)
	if err != nil {
		return nil, err
	}

	// Fanout + notifications (outside the transaction; PostgreSQL remains the
	// source of truth and clients re-sync via REST on any gap).
	memberIDs, _ := h.conv.MemberIDs(ctx, conversationID)
	recipients := []uuid.UUID{}
	var senderName, senderUsername string
	var senderAvatar *uuid.UUID
	_ = h.pool.QueryRow(ctx, `
		SELECT u.username, p.display_name, p.avatar_attachment_id FROM users u
		JOIN user_profiles p ON p.user_id = u.id WHERE u.id = $1
	`, senderID).Scan(&senderUsername, &senderName, &senderAvatar)

	mentions := extractMentions(msg.Text)
	for _, m := range memberIDs {
		if m == senderID {
			continue
		}
		recipients = append(recipients, m)

		// Notification preference check for groups (mentions_only).
		var level string
		var convType string
		_ = h.pool.QueryRow(ctx, `
			SELECT m.notification_level, c.type FROM conversation_members m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE m.conversation_id = $1 AND m.user_id = $2
		`, conversationID, m).Scan(&level, &convType)
		mentioned := contains(mentions, usernameFor(h.pool, ctx, m))
		if convType == "group" && level == "mentions_only" && !mentioned {
			continue
		}
		if level == "none" {
			continue
		}
		ntyp := "new_message"
		if mentioned {
			ntyp = "mention"
		}
		preview := previewFor(msg.Type, msg.Text)
		h.notify.Notify(ctx, m, ntyp, senderName, preview, &conversationID, &msgID, &senderID)
	}

	event := map[string]any{
		"type":     "message.created",
		"event_id": uuid.NewString(),
		"message":  msg,
	}
	h.hub.ToUsers(memberIDs, event, senderConn) // everyone incl. sender's other sessions

	return msg, nil
}

func usernameFor(pool *pgxpool.Pool, ctx context.Context, userID uuid.UUID) string {
	var u string
	_ = pool.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&u)
	return u
}

func extractMentions(text *string) []string {
	if text == nil {
		return nil
	}
	matches := mentionRe.FindAllStringSubmatch(strings.ToLower(*text), 10)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	var req SendRequest
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	msg, err := h.Send(r.Context(), convID, api.UserID(r.Context()), req, "")
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusCreated, msg)
}

// ---------------------------------------------------------------------------
// List (cursor pagination by sequence)
// ---------------------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())

	ok, err := h.conv.IsMember(r.Context(), convID, me)
	if err != nil || !ok {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n := atoiClamp(v, 1, 100); n > 0 {
			limit = n
		}
	}

	beforeSeq := int64(-1)
	afterSeq := int64(-1)
	if v := r.URL.Query().Get("before_seq"); v != "" {
		beforeSeq = atoi64(v)
	}
	if v := r.URL.Query().Get("after_seq"); v != "" {
		afterSeq = atoi64(v)
	}

	ctx := r.Context()
	rows, err := h.pool.Query(ctx, `
		SELECT id, seq FROM (
			SELECT id, seq FROM messages
			WHERE conversation_id = $1
			  AND ($2 < 0 OR seq < $2)
			  AND ($3 < 0 OR seq > $3)
			ORDER BY seq DESC
			LIMIT $4
		) recent ORDER BY seq ASC
	`, convID, beforeSeq, afterSeq, limit)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	var ids []uuid.UUID
	var oldestSeq int64 = -1
	first := true
	for rows.Next() {
		var id uuid.UUID
		var seq int64
		if err := rows.Scan(&id, &seq); err != nil {
			api.WriteError(w, r, err)
			return
		}
		if first {
			oldestSeq = seq
			first = false
		}
		ids = append(ids, id)
	}

	out := []Message{}
	for _, id := range ids {
		msg, err := h.Load(ctx, id, me)
		if err != nil {
			continue
		}
		out = append(out, *msg)
	}

	hasMore := false
	var nextCursor *string
	if oldestSeq > 0 {
		var cnt int
		if err := h.pool.QueryRow(ctx,
			`SELECT count(*) FROM messages WHERE conversation_id=$1 AND seq < $2`, convID, oldestSeq).Scan(&cnt); err == nil {
			hasMore = cnt > 0
		}
		if hasMore {
			s := itoa(oldestSeq)
			nextCursor = &s
		}
	}

	payload := map[string]any{
		"messages":    out,
		"next_cursor": nextCursor,
		"has_more":    hasMore,
	}
	api.WriteJSON(w, r, http.StatusOK, payload)
}

func (h *Handler) gallery(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	ok, err := h.conv.IsMember(r.Context(), convID, me)
	if err != nil || !ok {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}
	kindFilter := r.URL.Query().Get("kind")
	ctx := r.Context()

	rows, err := h.pool.Query(ctx, `
		SELECT m.id, m.type, m.created_at, m.sender_id, u.username, p.display_name, a.id, a.file_name, a.mime_type,
		       a.kind, a.size_bytes, a.width, a.height, a.duration_ms, a.thumb_key IS NOT NULL
		FROM messages m
		JOIN message_attachments ma ON ma.message_id = m.id
		JOIN attachments a ON a.id = ma.attachment_id
		JOIN users u ON u.id = m.sender_id
		JOIN user_profiles p ON p.user_id = m.sender_id
		WHERE m.conversation_id = $1 AND m.deleted_at IS NULL
		  AND ($2 = '' OR a.kind = $2)
		ORDER BY m.seq DESC
		LIMIT 200
	`, convID, kindFilter)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	type galleryItem struct {
		MessageID   string         `json:"message_id"`
		MessageType string         `json:"message_type"`
		CreatedAt   string         `json:"created_at"`
		Sender      string         `json:"sender"`
		Attachment  AttachmentMeta `json:"attachment"`
	}
	out := []galleryItem{}
	for rows.Next() {
		var it galleryItem
		var msgID, attID, senderID uuid.UUID
		var msgCreatedAt time.Time
		var fileName, mime, kind string
		var size int64
		var width, height, duration *int
		var hasThumb bool
		if err := rows.Scan(&msgID, &it.MessageType, &msgCreatedAt, &senderID, &it.Sender,
			&fileName, &mime, &kind, &size, &width, &height, &duration, &hasThumb); err != nil {
			api.WriteError(w, r, err)
			return
		}
		it.MessageID = msgID.String()
		it.CreatedAt = msgCreatedAt.UTC().Format(time.RFC3339)
		it.Attachment = AttachmentMeta{
			ID: attID.String(), Name: fileName, MimeType: mime, Kind: kind, SizeBytes: size,
			Width: width, Height: height, DurationMS: duration,
			DownloadURL: "/v1/attachments/" + attID.String() + "/download",
		}
		if kind == "image" {
			t := "/v1/attachments/" + attID.String() + "/thumbnail"
			it.Attachment.ThumbnailURL = &t
		}
		out = append(out, it)
	}
	api.WriteJSON(w, r, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// Edit / delete
// ---------------------------------------------------------------------------

func (h *Handler) edit(w http.ResponseWriter, r *http.Request) {
	msgID, err := uuid.Parse(chi.URLParam(r, "messageID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	var req struct {
		Text string `json:"text"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" || utf8.RuneCountInString(text) > 8000 {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Edited text must be 1-8,000 characters.", nil))
		return
	}
	ctx := r.Context()

	// Own message + edit window enforced server-side.
	var senderID uuid.UUID
	var convID uuid.UUID
	var createdAt time.Time
	var editWindow int
	err = h.pool.QueryRow(ctx, `
		SELECT m.sender_id, m.conversation_id, m.created_at, s.edit_window_seconds
		FROM messages m
		JOIN conversation_settings s ON s.conversation_id = m.conversation_id
		WHERE m.id = $1 AND m.deleted_at IS NULL AND m.type = 'text'
	`, msgID).Scan(&senderID, &convID, &createdAt, &editWindow)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.WriteError(w, r, api.ErrNotFound)
			return
		}
		api.WriteError(w, r, err)
		return
	}
	if senderID != me {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}
	if time.Since(createdAt) > time.Duration(editWindow)*time.Second {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"The edit window for this message has passed.", nil))
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(ctx)

	var oldText string
	if err := tx.QueryRow(ctx, `SELECT text FROM messages WHERE id = $1`, msgID).Scan(&oldText); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO message_edits (message_id, previous_text, new_text) VALUES ($1, $2, $3)`,
		msgID, oldText, text); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE messages SET text = $1, edited_at = now() WHERE id = $2`, text, msgID); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		api.WriteError(w, r, err)
		return
	}

	msg, err := h.Load(ctx, msgID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	memberIDs, _ := h.conv.MemberIDs(ctx, convID)
	h.hub.ToUsers(memberIDs, map[string]any{
		"type":     "message.updated",
		"event_id": uuid.NewString(),
		"message":  msg,
	}, "")
	api.WriteJSON(w, r, http.StatusOK, msg)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	msgID, err := uuid.Parse(chi.URLParam(r, "messageID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	ctx := r.Context()

	var senderID, convID uuid.UUID
	err = h.pool.QueryRow(ctx, `
		SELECT sender_id, conversation_id FROM messages WHERE id = $1 AND deleted_at IS NULL AND type <> 'system'
	`, msgID).Scan(&senderID, &convID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.WriteError(w, r, api.ErrNotFound)
			return
		}
		api.WriteError(w, r, err)
		return
	}
	if senderID != me {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}

	if _, err := h.pool.Exec(ctx,
		`UPDATE messages SET deleted_at = now(), deleted_by = $1, text = NULL WHERE id = $2`, me, msgID); err != nil {
		api.WriteError(w, r, err)
		return
	}

	memberIDs, _ := h.conv.MemberIDs(ctx, convID)
	h.hub.ToUsers(memberIDs, map[string]any{
		"type":            "message.deleted",
		"event_id":        uuid.NewString(),
		"conversation_id": convID.String(),
		"message_id":      msgID.String(),
	}, "")
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// ---------------------------------------------------------------------------
// Reactions
// ---------------------------------------------------------------------------

func (h *Handler) addReaction(w http.ResponseWriter, r *http.Request) {
	h.react(w, r, true)
}

func (h *Handler) removeReaction(w http.ResponseWriter, r *http.Request) {
	h.react(w, r, false)
}

func (h *Handler) react(w http.ResponseWriter, r *http.Request, add bool) {
	msgID, err := uuid.Parse(chi.URLParam(r, "messageID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	emoji := strings.TrimSpace(req.Emoji)
	if utf8.RuneCountInString(emoji) < 1 || utf8.RuneCountInString(emoji) > 8 {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Invalid reaction emoji.", nil))
		return
	}
	ctx := r.Context()

	var convID uuid.UUID
	err = h.pool.QueryRow(ctx,
		`SELECT conversation_id FROM messages WHERE id = $1 AND deleted_at IS NULL`, msgID).Scan(&convID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.WriteError(w, r, api.ErrNotFound)
			return
		}
		api.WriteError(w, r, err)
		return
	}
	ok, err := h.conv.IsMember(ctx, convID, me)
	if err != nil || !ok {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}

	if add {
		if _, err := h.pool.Exec(ctx,
			`INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			msgID, me, emoji); err != nil {
			api.WriteError(w, r, err)
			return
		}
	} else {
		if _, err := h.pool.Exec(ctx,
			`DELETE FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3`,
			msgID, me, emoji); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}

	evtType := "reaction.removed"
	if add {
		evtType = "reaction.added"
	}
	memberIDs, _ := h.conv.MemberIDs(ctx, convID)
	h.hub.ToUsers(memberIDs, map[string]any{
		"type":            evtType,
		"event_id":        uuid.NewString(),
		"message_id":      msgID.String(),
		"conversation_id": convID.String(),
		"user_id":         me.String(),
		"emoji":           emoji,
	}, "")
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// Receipts detail
// ---------------------------------------------------------------------------

func (h *Handler) receipts(w http.ResponseWriter, r *http.Request) {
	msgID, err := uuid.Parse(chi.URLParam(r, "messageID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	ctx := r.Context()

	var convID, senderID uuid.UUID
	var msgSeq int64
	err = h.pool.QueryRow(ctx,
		`SELECT conversation_id, sender_id, seq FROM messages WHERE id = $1`, msgID).Scan(&convID, &senderID, &msgSeq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.WriteError(w, r, api.ErrNotFound)
			return
		}
		api.WriteError(w, r, err)
		return
	}
	ok, err := h.conv.IsMember(ctx, convID, me)
	if err != nil || !ok {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}

	rows, err := h.pool.Query(ctx, `
		SELECT u.id, u.username, p.display_name, p.avatar_attachment_id, mr.delivered_at, mr.read_at, c.read_seq
		FROM conversation_members cm
		JOIN users u ON u.id = cm.user_id
		JOIN user_profiles p ON p.user_id = cm.user_id
		JOIN message_read_cursors c ON c.conversation_id = cm.conversation_id AND c.user_id = cm.user_id
		LEFT JOIN message_receipts mr ON mr.message_id = $1 AND mr.user_id = cm.user_id
		WHERE cm.conversation_id = $2 AND cm.left_at IS NULL AND cm.user_id <> $3
	`, msgID, convID, me)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	type receiptRow struct {
		UserID      string  `json:"user_id"`
		Username    string  `json:"username"`
		DisplayName string  `json:"display_name"`
		AvatarURL   *string `json:"avatar_url,omitempty"`
		DeliveredAt *string `json:"delivered_at,omitempty"`
		ReadAt      *string `json:"read_at,omitempty"`
		Read        bool    `json:"read"`
		Delivered   bool    `json:"delivered"`
	}
	out := []receiptRow{}
	for rows.Next() {
		var rr receiptRow
		var id uuid.UUID
		var avatarID *uuid.UUID
		var deliveredAt, readAt *time.Time
		var readSeq int64
		if err := rows.Scan(&id, &rr.Username, &rr.DisplayName, &avatarID, &deliveredAt, &readAt, &readSeq); err != nil {
			api.WriteError(w, r, err)
			return
		}
		rr.UserID = id.String()
		if avatarID != nil {
			url := "/v1/attachments/" + avatarID.String() + "/download"
			rr.AvatarURL = &url
		}
		rr.Read = readSeq >= msgSeq
		rr.Delivered = rr.Read || deliveredAt != nil
		if rr.Read && readAt != nil {
			ts := readAt.UTC().Format(time.RFC3339)
			rr.ReadAt = &ts
		}
		if rr.Delivered && deliveredAt != nil {
			ts := deliveredAt.UTC().Format(time.RFC3339)
			rr.DeliveredAt = &ts
		}
		out = append(out, rr)
	}
	api.WriteJSON(w, r, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// Load (full message projection)
// ---------------------------------------------------------------------------

// Load projects a complete message for a viewer, including reply preview,
// attachments, reactions and receipts (for the sender's own messages).
func (h *Handler) Load(ctx context.Context, msgID, viewer uuid.UUID) (*Message, error) {
	var m Message
	var senderID *uuid.UUID
	var convID uuid.UUID
	var msgType string
	var text *string
	var seq int64
	var createdAt time.Time
	var editedAt, deletedAt *time.Time
	var replyToID *uuid.UUID
	var clientMsgID *uuid.UUID

	err := h.pool.QueryRow(ctx, `
		SELECT id, conversation_id, sender_id, type, text, seq, created_at, edited_at, deleted_at, reply_to_id, client_message_id
		FROM messages WHERE id = $1
	`, msgID).Scan(&m.ID, &convID, &senderID, &msgType, &text, &seq, &createdAt, &editedAt, &deletedAt, &replyToID, &clientMsgID)
	if err != nil {
		return nil, err
	}

	m.ConversationID = convID.String()
	m.Type = msgType
	m.Seq = seq
	m.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	m.Deleted = deletedAt != nil
	if deletedAt != nil {
		m.Text = nil // tombstone
	} else {
		m.Text = text
	}
	if editedAt != nil {
		ts := editedAt.UTC().Format(time.RFC3339Nano)
		m.EditedAt = &ts
	}
	if clientMsgID != nil {
		m.ClientMessageID = clientMsgID.String()
	}
	m.IsMine = senderID != nil && *senderID == viewer

	if senderID != nil {
		var username, display string
		var avatarID *uuid.UUID
		if err := h.pool.QueryRow(ctx, `
			SELECT u.username, p.display_name, p.avatar_attachment_id FROM users u
			JOIN user_profiles p ON p.user_id = u.id WHERE u.id = $1
		`, *senderID).Scan(&username, &display, &avatarID); err == nil {
			s := &Sender{ID: senderID.String(), Username: username, DisplayName: display}
			if avatarID != nil {
				url := "/v1/attachments/" + avatarID.String() + "/download"
				s.AvatarURL = &url
			}
			m.Sender = s
		}
	}

	// Reply preview.
	if replyToID != nil {
		var rp ReplyPreview
		var rt string
		var rtext *string
		var rname *string
		if err := h.pool.QueryRow(ctx, `
			SELECT m.id, m.type, m.text, p.display_name
			FROM messages m
			LEFT JOIN user_profiles p ON p.user_id = m.sender_id
			WHERE m.id = $1
		`, *replyToID).Scan(&rp.ID, &rt, &rtext, &rname); err == nil {
			rp.Type = rt
			rp.SenderName = deref(rname)
			if deletedAt == nil {
				rp.Preview = previewFor(rt, rtext)
			} else {
				rp.Preview = "Deleted message"
			}
			m.ReplyTo = &rp
		}
	}

	// Attachments.
	rows, err := h.pool.Query(ctx, `
		SELECT a.id, a.file_name, a.mime_type, a.kind, a.size_bytes, a.width, a.height, a.duration_ms, a.thumb_key IS NOT NULL
		FROM message_attachments ma
		JOIN attachments a ON a.id = ma.attachment_id
		WHERE ma.message_id = $1
		ORDER BY ma.position
	`, msgID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var am AttachmentMeta
			var attID uuid.UUID
			var hasThumb bool
			if err := rows.Scan(&attID, &am.Name, &am.MimeType, &am.Kind, &am.SizeBytes,
				&am.Width, &am.Height, &am.DurationMS, &hasThumb); err != nil {
				break
			}
			am.ID = attID.String()
			am.DownloadURL = "/v1/attachments/" + attID.String() + "/download"
			if am.Kind == "image" && hasThumb {
				t := "/v1/attachments/" + attID.String() + "/thumbnail"
				am.ThumbnailURL = &t
			}
			m.Attachments = append(m.Attachments, am)
		}
	}

	// Reactions.
	rrows, err := h.pool.Query(ctx, `
		SELECT emoji, count(*), array_agg(u.id::text) FROM message_reactions r
		JOIN users u ON u.id = r.user_id
		WHERE r.message_id = $1
		GROUP BY emoji ORDER BY min(r.created_at)
	`, msgID)
	if err == nil {
		defer rrows.Close()
		for rrows.Next() {
			var rc Reaction
			var ids []string
			if err := rrows.Scan(&rc.Emoji, &rc.Count, &ids); err != nil {
				break
			}
			rc.UserIDs = ids
			for _, id := range ids {
				if id == viewer.String() {
					rc.Mine = true
				}
			}
			m.Reactions = append(m.Reactions, rc)
		}
	}

	// Receipts for own messages (delivered/read counts among other members).
	if m.IsMine {
		rc := &Receipts{}
		var otherMembers int
		_ = h.pool.QueryRow(ctx, `
			SELECT count(*) FROM conversation_members
			WHERE conversation_id = $1 AND left_at IS NULL AND user_id <> $2
		`, convID, viewer).Scan(&otherMembers)
		rc.Total = otherMembers
		_ = h.pool.QueryRow(ctx, `
			SELECT count(*) FROM message_read_cursors c
			JOIN conversation_members cm ON cm.conversation_id = c.conversation_id AND cm.user_id = c.user_id
			WHERE c.conversation_id = $1 AND c.user_id <> $2 AND cm.left_at IS NULL AND c.read_seq >= $3
		`, convID, viewer, seq).Scan(&rc.ReadCount)
		_ = h.pool.QueryRow(ctx, `
			SELECT count(*) FROM message_read_cursors c
			JOIN conversation_members cm ON cm.conversation_id = c.conversation_id AND cm.user_id = c.user_id
			WHERE c.conversation_id = $1 AND c.user_id <> $2 AND cm.left_at IS NULL AND (c.delivered_seq >= $3 OR c.read_seq >= $3)
		`, convID, viewer, seq).Scan(&rc.DeliveredCount)
		m.Receipts = rc
	}

	return &m, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func atoiClamp(s string, min, max int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
		if n > max {
			return max
		}
	}
	if n < min {
		return min
	}
	return n
}

func atoi64(s string) int64 {
	var n int64
	neg := false
	for i, c := range s {
		if i == 0 && c == '-' {
			neg = true
			continue
		}
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		return -n
	}
	return n
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// MarshalEvent is a tiny helper for consistent WS event encoding.
func MarshalEvent(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
