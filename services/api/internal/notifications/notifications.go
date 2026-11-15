// Package notifications implements the local in-app notification center.
package notifications

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singhtanishq/veylo/services/api/internal/api"
)

// Fanout delivers a notification event to a user's live sessions.
type Fanout interface {
	ToUser(userID uuid.UUID, payload any)
}

// Notifier writes notification rows and fans out live events. Notification
// preference filtering is applied by callers that know the notification kind.
type Notifier struct {
	pool   *pgxpool.Pool
	fanout Fanout
}

// New builds a Notifier.
func New(pool *pgxpool.Pool, f Fanout) *Notifier { return &Notifier{pool: pool, fanout: f} }

// SetFanout wires the realtime hub after construction.
func (n *Notifier) SetFanout(f Fanout) { n.fanout = f }

// Notification is the API shape of a notification.
type Notification struct {
	ID             string  `json:"id"`
	Type           string  `json:"type"`
	Title          string  `json:"title"`
	Body           *string `json:"body"`
	ConversationID *string `json:"conversation_id,omitempty"`
	MessageID      *string `json:"message_id,omitempty"`
	Actor          *Actor  `json:"actor,omitempty"`
	ReadAt         *string `json:"read_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
}

// Actor is the minimal user projection attached to notifications.
type Actor struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
}

// Notify inserts a notification row and fans out a live event (best effort).
func (n *Notifier) Notify(ctx context.Context, userID uuid.UUID, typ, title, body string,
	conversationID, messageID, actorID *uuid.UUID) {
	var convID, msgID *string
	if conversationID != nil {
		s := conversationID.String()
		convID = &s
	}
	if messageID != nil {
		s := messageID.String()
		msgID = &s
	}

	notif := Notification{
		Type:           typ,
		Title:          title,
		ConversationID: convID,
		MessageID:      msgID,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	if body != "" {
		notif.Body = &body
	}

	var createdAt time.Time
	var retActor *uuid.UUID
	err := n.pool.QueryRow(ctx, `
		INSERT INTO notifications (user_id, type, title, body, conversation_id, message_id, actor_id)
		VALUES ($1, $2, $3, NULLIF($4,''), $5, $6, $7)
		RETURNING id, created_at, actor_id
	`, userID, typ, title, body, convID, msgID, actorID).
		Scan(&notif.ID, &createdAt, &retActor)
	if err == nil {
		notif.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		if retActor != nil {
			var username, displayName string
			var avatarID *uuid.UUID
			if err := n.pool.QueryRow(ctx, `
				SELECT u.username, p.display_name, p.avatar_attachment_id
				FROM users u JOIN user_profiles p ON p.user_id = u.id
				WHERE u.id = $1
			`, *retActor).Scan(&username, &displayName, &avatarID); err == nil {
				a := &Actor{ID: retActor.String(), Username: username, DisplayName: displayName}
				if avatarID != nil {
					url := "/v1/attachments/" + avatarID.String() + "/download"
					a.AvatarURL = &url
				}
				notif.Actor = a
			}
		}
		if n.fanout != nil {
			n.fanout.ToUser(userID, map[string]any{
				"type":         "notification.created",
				"event_id":     uuid.NewString(),
				"notification": notif,
			})
		}
	}
	// Notification failures are best-effort; never fail the triggering action.
}

// Handler exposes notification endpoints (auth applied by router).
type Handler struct {
	pool *pgxpool.Pool
}

// NewHandler constructs the notifications handler.
func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

// Routes registers notification endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/notifications", h.list)
	r.Post("/notifications/read", h.markRead)
}

type actorRow struct {
	ID          uuid.UUID
	Username    string
	DisplayName string
	AvatarID    *uuid.UUID
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	me := api.UserID(r.Context())
	unreadOnly := r.URL.Query().Get("unread") == "true"
	limit := 50

	args := []any{me}
	where := "n.user_id = $1"
	if unreadOnly {
		where += " AND n.read_at IS NULL"
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT n.id, n.type, n.title, n.body, n.conversation_id, n.message_id, n.read_at, n.created_at,
		       n.actor_id, u.username, p.display_name, p.avatar_attachment_id
		FROM notifications n
		LEFT JOIN users u ON u.id = n.actor_id
		LEFT JOIN user_profiles p ON p.user_id = n.actor_id
		WHERE `+where+`
		ORDER BY n.created_at DESC
		LIMIT $2
	`, append(args, limit)...)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	out := []Notification{}
	for rows.Next() {
		var n Notification
		var id, convID, msgID, actorID *uuid.UUID
		var body *string
		var readAt *time.Time
		var createdAt time.Time
		var actorUsername, actorDisplay *string
		var actorAvatar *uuid.UUID
		if err := rows.Scan(&id, &n.Type, &n.Title, &body, &convID, &msgID, &readAt, &createdAt,
			&actorID, &actorUsername, &actorDisplay, &actorAvatar); err != nil {
			api.WriteError(w, r, err)
			return
		}
		n.ID = id.String()
		n.Body = body
		if convID != nil {
			s := convID.String()
			n.ConversationID = &s
		}
		if msgID != nil {
			s := msgID.String()
			n.MessageID = &s
		}
		if readAt != nil {
			s := readAt.UTC().Format(time.RFC3339)
			n.ReadAt = &s
		}
		n.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		if actorID != nil && actorUsername != nil {
			a := &Actor{ID: actorID.String(), Username: *actorUsername, DisplayName: deref(actorDisplay)}
			if actorAvatar != nil {
				url := "/v1/attachments/" + actorAvatar.String() + "/download"
				a.AvatarURL = &url
			}
			n.Actor = a
		}
		out = append(out, n)
	}
	api.WriteJSON(w, r, http.StatusOK, out)
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	me := api.UserID(r.Context())
	ctx := r.Context()

	if req.All {
		if _, err := h.pool.Exec(ctx,
			`UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, me); err != nil {
			api.WriteError(w, r, err)
			return
		}
		api.WriteJSON(w, r, http.StatusOK, map[string]any{"marked": "all"})
		return
	}

	ids := make([]uuid.UUID, 0, len(req.IDs))
	for _, raw := range req.IDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			api.WriteError(w, r, api.ErrBadRequest)
			return
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Provide notification ids or all=true.", nil))
		return
	}
	tag, err := h.pool.Exec(ctx, `
		UPDATE notifications SET read_at = now()
		WHERE user_id = $1 AND read_at IS NULL AND id = ANY($2)
	`, me, ids)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"marked": tag.RowsAffected()})
}

// UnreadCount returns the number of unread notifications for a user.
func (h *Handler) UnreadCount(ctx context.Context, userID uuid.UUID) int {
	var n int
	if err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&n); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0
		}
		return 0
	}
	return n
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
