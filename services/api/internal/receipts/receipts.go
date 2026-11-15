// Package receipts implements delivery/read receipt semantics:
//   - Sent:      the server persisted the message (ack to sender).
//   - Delivered: a recipient session acknowledged receiving the event.
//   - Read:      the recipient advanced their read cursor explicitly.
//
// Per-user-per-conversation cursors are authoritative for unread counts;
// per-message receipt rows add per-message timestamps for the UI.
package receipts

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singhtanishq/veylo/services/api/internal/api"
)

// Hub fans receipt updates to conversation members.
type Hub interface {
	ToUsers(userIDs []uuid.UUID, payload any, excludeConn string)
}

// Handler implements receipt writes.
type Handler struct {
	pool        *pgxpool.Pool
	hub         Hub
	readsHidden func(ctx context.Context, userID uuid.UUID) bool
}

// NewHandler constructs the receipts handler. readsHidden reports whether a
// user disabled read receipts (their reads stay private).
func NewHandler(pool *pgxpool.Pool, hub Hub, readsHiddenFn func(ctx context.Context, userID uuid.UUID) bool) *Handler {
	return &Handler{pool: pool, hub: hub, readsHidden: readsHiddenFn}
}

// SetHub wires the realtime hub after construction.
func (h *Handler) SetHub(hub Hub) { h.hub = hub }

// MarkDelivered advances the delivered cursor up to maxSeq (monotonic).
func (h *Handler) MarkDelivered(ctx context.Context, conversationID, userID uuid.UUID, maxSeq int64) error {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var prev int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO message_read_cursors (conversation_id, user_id, delivered_seq)
		VALUES ($1, $2, $3)
		ON CONFLICT (conversation_id, user_id) DO UPDATE SET updated_at = now()
		RETURNING delivered_seq
	`, conversationID, userID, maxSeq).Scan(&prev); err != nil {
		return err
	}

	newVal := maxSeq
	if prev > maxSeq {
		newVal = prev
	}
	if _, err := tx.Exec(ctx, `
		UPDATE message_read_cursors SET delivered_seq = $3, updated_at = now()
		WHERE conversation_id = $1 AND user_id = $2 AND delivered_seq < $3
	`, conversationID, userID, newVal); err != nil {
		return err
	}

	// Write per-message delivered receipts for the newly covered range.
	if _, err := tx.Exec(ctx, `
		INSERT INTO message_receipts (message_id, user_id, delivered_at)
		SELECT m.id, $2, now() FROM messages m
		WHERE m.conversation_id = $1 AND m.seq > $3 AND m.seq <= $4 AND m.sender_id IS DISTINCT FROM $2
		ON CONFLICT (message_id, user_id) DO UPDATE SET delivered_at = COALESCE(message_receipts.delivered_at, now())
	`, conversationID, userID, prev, newVal); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Notify the conversation members (esp. the sender) — best effort.
	h.notifyMembers(ctx, conversationID, map[string]any{
		"type":            "receipt.delivered",
		"event_id":        uuid.NewString(),
		"conversation_id": conversationID.String(),
		"user_id":         userID.String(),
		"max_seq":         newVal,
	})
	return nil
}

// MarkRead advances the read cursor up to maxSeq (monotonic) and emits read
// receipts unless the reader disabled them.
func (h *Handler) MarkRead(ctx context.Context, conversationID, userID uuid.UUID, maxSeq int64) error {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var prev int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO message_read_cursors (conversation_id, user_id, read_seq)
		VALUES ($1, $2, $3)
		ON CONFLICT (conversation_id, user_id) DO UPDATE SET updated_at = now()
		RETURNING read_seq
	`, conversationID, userID, maxSeq).Scan(&prev); err != nil {
		return err
	}

	newVal := maxSeq
	if prev > maxSeq {
		newVal = prev
	}
	if _, err := tx.Exec(ctx, `
		UPDATE message_read_cursors SET read_seq = $3, updated_at = now()
		WHERE conversation_id = $1 AND user_id = $2 AND read_seq < $3
	`, conversationID, userID, newVal); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE message_read_cursors SET delivered_seq = GREATEST(delivered_seq, $3), updated_at = now()
		WHERE conversation_id = $1 AND user_id = $2 AND delivered_seq < $3
	`, conversationID, userID, newVal); err != nil {
		return err
	}

	// Reading implies delivery.
	if _, err := tx.Exec(ctx, `
		INSERT INTO message_receipts (message_id, user_id, delivered_at, read_at)
		SELECT m.id, $2, now(), now() FROM messages m
		WHERE m.conversation_id = $1 AND m.seq > $3 AND m.seq <= $4 AND m.sender_id IS DISTINCT FROM $2
		ON CONFLICT (message_id, user_id) DO UPDATE SET
			read_at = COALESCE(message_receipts.read_at, now()),
			delivered_at = COALESCE(message_receipts.delivered_at, now())
	`, conversationID, userID, prev, newVal); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if h.readsHidden == nil || !h.readsHidden(ctx, userID) {
		h.notifyMembers(ctx, conversationID, map[string]any{
			"type":            "receipt.read",
			"event_id":        uuid.NewString(),
			"conversation_id": conversationID.String(),
			"user_id":         userID.String(),
			"max_seq":         newVal,
		})
	}
	return nil
}

func (h *Handler) notifyMembers(ctx context.Context, conversationID uuid.UUID, payload map[string]any) {
	if h.hub == nil {
		return
	}
	rows, err := h.pool.Query(ctx, `
		SELECT user_id FROM conversation_members WHERE conversation_id = $1 AND left_at IS NULL
	`, conversationID)
	if err != nil {
		return
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	h.hub.ToUsers(ids, payload, "")
}

// ReadSeq returns the user's read cursor for a conversation.
func (h *Handler) ReadSeq(ctx context.Context, conversationID, userID uuid.UUID) (int64, error) {
	var seq int64
	err := h.pool.QueryRow(ctx, `
		SELECT read_seq FROM message_read_cursors WHERE conversation_id = $1 AND user_id = $2
	`, conversationID, userID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return seq, nil
}

// CursorUpdated reflects monotonic cursor change info (helper for tests).
func CursorUpdated(prev, next int64) bool {
	return next > prev && next >= 0 && time.Since(time.Unix(0, 0)) > 0
}

// ---------------------------------------------------------------------------
// REST endpoints
// ---------------------------------------------------------------------------

// Routes registers receipt endpoints (auth applied by router).
func (h *Handler) Routes(r chi.Router) {
	r.Post("/conversations/{conversationID}/read", h.readREST)
}

// readREST advances the caller's read cursor (REST counterpart of the WS
// receipt.read event).
func (h *Handler) readREST(w http.ResponseWriter, r *http.Request) {
	convID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	var req struct {
		MaxSeq int64 `json:"max_seq"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if req.MaxSeq < 0 {
		api.WriteError(w, r, api.ErrValidation)
		return
	}
	me := api.UserID(r.Context())

	var member bool
	if err := h.pool.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=$1 AND user_id=$2 AND left_at IS NULL)
	`, convID, me).Scan(&member); err != nil || !member {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}

	if err := h.MarkRead(r.Context(), convID, me, req.MaxSeq); err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"read_seq": req.MaxSeq})
}
