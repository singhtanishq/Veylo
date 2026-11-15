// Package realtime implements the authenticated WebSocket layer: a connection
// hub with per-user session fanout, heartbeats, one-time ticket authentication
// and the versioned Veylo event protocol.
//
// Protocol (v1) — JSON envelopes:
//
//	{ "v": 1, "type": "<event>", "event_id": "<uuid>", "ts": "<rfc3339>", "data": {...} }
//
// Client → server: ping, typing.start, typing.stop, receipt.deliver,
// receipt.read, message.send
//
// Server → client: connection.ready, pong, message.created, message.updated,
// message.deleted, message.failed, message.sent (ack), receipt.delivered,
// receipt.read, typing.start, typing.stop, presence.online, presence.offline,
// conversation.created, conversation.updated, conversation.deleted,
// conversation.member_added, conversation.member_removed, reaction.added,
// reaction.removed, notification.created
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/singhtanishq/veylo/services/api/internal/api"
	"github.com/singhtanishq/veylo/services/api/internal/messages"
	"github.com/singhtanishq/veylo/services/api/internal/platform/redisx"
	"github.com/singhtanishq/veylo/services/api/internal/presence"
)

const (
	// Protocol version emitted in every envelope.
	ProtocolVersion = 1

	writeWait  = 10 * time.Second
	pongWait   = 75 * time.Second
	pingPeriod = 30 * time.Second // must be < pongWait
	sendBuffer = 256
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Origin is verified by the ticket (a one-time Redis-backed token), so any
	// origin may start the handshake; unauthenticated upgrades are rejected.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Envelope is the v1 protocol frame.
type Envelope struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	EventID string          `json:"event_id,omitempty"`
	TS      string          `json:"ts,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func serverEnvelope(eventType string, payload any) Envelope {
	data, _ := json.Marshal(payload)
	return Envelope{
		V:       ProtocolVersion,
		Type:    eventType,
		EventID: uuid.NewString(),
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Data:    data,
	}
}

// Conn is one authenticated WebSocket connection.
type Conn struct {
	ID        string
	UserID    uuid.UUID
	SessionID uuid.UUID
	hub       *Hub
	ws        *websocket.Conn
	send      chan Envelope
	ctx       context.Context
	cancel    context.CancelFunc
}

// Hub tracks all live connections and fans out events.
type Hub struct {
	mu    sync.RWMutex
	conns map[uuid.UUID]map[string]*Conn

	pool     *pgxpool.Pool
	rdb      *redis.Client
	presence *presence.Store
	log      *slog.Logger

	messages   MessagePersister
	receipts   ReceiptPersister
	readHidden ReadPrivacyChecker
	ctx        context.Context
	cancel     context.CancelFunc
}

// MessagePersister persists incoming WS messages (implemented by messages pkg).
type MessagePersister interface {
	Send(ctx context.Context, conversationID, senderID uuid.UUID, req messages.SendRequest, senderConn string) (*messages.Message, error)
}

// ReceiptPersister advances cursors (implemented by receipts pkg).
type ReceiptPersister interface {
	MarkDelivered(ctx context.Context, conversationID, userID uuid.UUID, maxSeq int64) error
	MarkRead(ctx context.Context, conversationID, userID uuid.UUID, maxSeq int64) error
}

// ReadPrivacyChecker reports whether a user has read receipts disabled.
type ReadPrivacyChecker func(ctx context.Context, userID uuid.UUID) bool

// NewHub builds the hub.
func NewHub(pool *pgxpool.Pool, rdb *redis.Client, p *presence.Store, log *slog.Logger,
	messages MessagePersister, receipts ReceiptPersister, readHidden ReadPrivacyChecker) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		conns:      make(map[uuid.UUID]map[string]*Conn),
		pool:       pool,
		rdb:        rdb,
		presence:   p,
		log:        log,
		messages:   messages,
		receipts:   receipts,
		readHidden: readHidden,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// ---------------------------------------------------------------------------
// Connection registration / presence transitions
// ---------------------------------------------------------------------------

func (h *Hub) register(c *Conn) {
	h.mu.Lock()
	if h.conns[c.UserID] == nil {
		h.conns[c.UserID] = make(map[string]*Conn)
	}
	first := len(h.conns[c.UserID]) == 0
	h.conns[c.UserID][c.ID] = c
	h.mu.Unlock()

	if first {
		_ = h.presence.SetOnline(h.ctx, c.UserID)
		go h.announcePresence(c.UserID, true)
	}
}

func (h *Hub) unregister(c *Conn) {
	h.mu.Lock()
	if m := h.conns[c.UserID]; m != nil {
		if _, ok := m[c.ID]; ok {
			delete(m, c.ID)
		}
		if len(m) == 0 {
			delete(h.conns, c.UserID)
		}
	}
	last := len(h.conns[c.UserID]) == 0
	h.mu.Unlock()

	if last {
		_ = h.presence.SetOffline(h.ctx, c.UserID)
		// Record last seen + notify watchers.
		_, _ = h.pool.Exec(h.ctx, `UPDATE users SET last_seen_at = now() WHERE id = $1`, c.UserID)
		go h.announcePresence(c.UserID, false)
	}
}

// announcePresence fans presence changes to co-members and contacts, filtered
// by the subject's online visibility and block relationships.
func (h *Hub) announcePresence(userID uuid.UUID, online bool) {
	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()

	// Respect the subject's own online visibility setting.
	var onlineVis string
	if err := h.pool.QueryRow(ctx,
		`SELECT online_visibility FROM user_privacy_settings WHERE user_id = $1`, userID).Scan(&onlineVis); err != nil {
		return
	}
	if onlineVis == "nobody" {
		return
	}

	// Audience: contacts + co-members of active conversations, minus blocked
	// relationships in either direction.
	rows, err := h.pool.Query(ctx, `
		SELECT DISTINCT u.id FROM users u
		WHERE u.id IN (
			SELECT contact_id FROM contacts WHERE owner_id = $1
			UNION
			SELECT owner_id FROM contacts WHERE contact_id = $1
			UNION
			SELECT user_id FROM conversation_members
			WHERE conversation_id IN (
				SELECT conversation_id FROM conversation_members WHERE user_id = $1 AND left_at IS NULL
			) AND left_at IS NULL
		)
		AND u.id <> $1
		AND NOT EXISTS (SELECT 1 FROM blocked_users WHERE blocker_id = u.id AND blocked_id = $1)
		AND NOT EXISTS (SELECT 1 FROM blocked_users WHERE blocker_id = $1 AND blocked_id = u.id)
	`, userID)
	if err != nil {
		return
	}
	defer rows.Close()
	var audience []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			audience = append(audience, id)
		}
	}
	if len(audience) == 0 {
		return
	}

	// For contacts-only visibility, filter the audience to actual contacts.
	if onlineVis == "contacts" {
		var contactsOnly []uuid.UUID
		for _, a := range audience {
			var isContact bool
			if err := h.pool.QueryRow(ctx, `
				SELECT EXISTS(SELECT 1 FROM contacts WHERE (owner_id=$1 AND contact_id=$2) OR (owner_id=$2 AND contact_id=$1))
			`, userID, a).Scan(&isContact); err == nil && isContact {
				contactsOnly = append(contactsOnly, a)
			}
		}
		audience = contactsOnly
		if len(audience) == 0 {
			return
		}
	}

	eventType := "presence.offline"
	payload := map[string]any{"user_id": userID.String()}
	if online {
		eventType = "presence.online"
	} else {
		var lastSeen *time.Time
		_ = h.pool.QueryRow(ctx, `SELECT last_seen_at FROM users WHERE id = $1`, userID).Scan(&lastSeen)
		if lastSeen != nil {
			payload["last_seen_at"] = lastSeen.UTC().Format(time.RFC3339)
		}
	}
	h.ToUsers(audience, serverEnvelope(eventType, payload), "")
}

// ---------------------------------------------------------------------------
// Fanout primitives (implement conversations.Hub, messages.Hub, receipts.Hub,
// contacts.Fanout, notifications.Fanout)
// ---------------------------------------------------------------------------

// ToUser delivers an event to all sessions of one user.
func (h *Hub) ToUser(userID uuid.UUID, payload any) {
	h.mu.RLock()
	conns := make([]*Conn, 0, len(h.conns[userID]))
	for _, c := range h.conns[userID] {
		conns = append(conns, c)
	}
	h.mu.RUnlock()
	var env Envelope
	switch p := payload.(type) {
	case Envelope:
		env = p
	default:
		env = serverEnvelope("event", payload)
		if m, ok := payload.(map[string]any); ok {
			if t, ok := m["type"].(string); ok && t != "" {
				env.Type = t
			}
		}
	}
	for _, c := range conns {
		c.deliver(env)
	}
}

// ToUsers delivers an event to all sessions of many users, skipping one conn.
func (h *Hub) ToUsers(userIDs []uuid.UUID, payload any, excludeConn string) {
	var env Envelope
	switch p := payload.(type) {
	case Envelope:
		env = p
	default:
		env = serverEnvelope("event", payload)
		if m, ok := payload.(map[string]any); ok {
			if t, ok := m["type"].(string); ok && t != "" {
				env.Type = t
			}
		}
	}
	h.mu.RLock()
	var targets []*Conn
	for _, id := range userIDs {
		for _, c := range h.conns[id] {
			if c.ID == excludeConn {
				continue
			}
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		c.deliver(env)
	}
}

// IsOnline reports whether the user has any live connection.
func (h *Hub) IsOnline(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns[userID]) > 0
}

// Broadcast delivers to every connection (system-wide announcements; unused in
// normal traffic but available for maintenance notices).
func (h *Hub) Broadcast(payload any) {
	env := serverEnvelope("system.broadcast", payload)
	h.mu.RLock()
	var targets []*Conn
	for _, m := range h.conns {
		for _, c := range m {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		c.deliver(env)
	}
}

func (c *Conn) deliver(env Envelope) {
	select {
	case c.send <- env:
	default:
		// Slow consumer: drop the connection; the client re-syncs on reconnect.
		c.cancel()
	}
}

// ---------------------------------------------------------------------------
// WebSocket endpoint + ticket auth
// ---------------------------------------------------------------------------

// Handler exposes the WS endpoint and ticket issuer.
type Handler struct {
	hub *Hub
}

// NewHandler builds the WS handler.
func NewHandler(hub *Hub) *Handler { return &Handler{hub: hub} }

// IssueTicket creates a one-time 30s ticket bound to the caller's session.
func (h *Hub) IssueTicket(ctx context.Context, userID, sessionID uuid.UUID) (string, error) {
	ticket := uuid.NewString() + "-" + uuid.NewString()
	payload, _ := json.Marshal(map[string]string{
		"user_id":    userID.String(),
		"session_id": sessionID.String(),
	})
	if err := h.rdb.Set(ctx, redisx.KeyWSTicket+ticket, payload, 30*time.Second).Err(); err != nil {
		return "", err
	}
	return ticket, nil
}

// TokenRoutes registers POST /ws/token (requires cookie auth middleware).
func (h *Handler) TokenRoutes(r chi.Router) {
	r.Post("/ws/token", h.issueToken)
}

// WSRoutes registers GET /ws (public; authenticated via one-time ticket).
func (h *Handler) WSRoutes(r chi.Router) {
	r.Get("/ws", h.serveWS)
}

func (h *Handler) issueToken(w http.ResponseWriter, r *http.Request) {
	ticket, err := h.hub.IssueTicket(r.Context(), api.UserID(r.Context()), api.SessionID(r.Context()))
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{
		"ticket":     ticket,
		"expires_in": 30,
	})
}

func (h *Handler) serveWS(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" {
		http.Error(w, "missing ticket", http.StatusUnauthorized)
		return
	}
	// Consume the one-time ticket atomically.
	val, err := h.hub.rdb.GetDel(h.hub.ctx, redisx.KeyWSTicket+ticket).Result()
	if err != nil {
		http.Error(w, "invalid or expired ticket", http.StatusUnauthorized)
		return
	}
	var auth struct {
		UserID    string `json:"user_id"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal([]byte(val), &auth); err != nil {
		http.Error(w, "invalid ticket payload", http.StatusUnauthorized)
		return
	}
	userID, err := uuid.Parse(auth.UserID)
	if err != nil {
		http.Error(w, "invalid ticket payload", http.StatusUnauthorized)
		return
	}
	sessionID, _ := uuid.Parse(auth.SessionID)

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	ctx, cancel := context.WithCancel(h.hub.ctx)
	conn := &Conn{
		ID:        uuid.NewString(),
		UserID:    userID,
		SessionID: sessionID,
		hub:       h.hub,
		ws:        ws,
		send:      make(chan Envelope, sendBuffer),
		ctx:       ctx,
		cancel:    cancel,
	}
	h.hub.register(conn)
	h.hub.log.Info("ws connected", "user", userID.String(), "conn", conn.ID)

	go conn.writePump()
	go conn.readPump()

	// Greet the connection with server state (time + identity).
	conn.deliver(serverEnvelope("connection.ready", map[string]any{
		"user_id":       userID.String(),
		"conn_id":       conn.ID,
		"server_time":   time.Now().UTC().Format(time.RFC3339Nano),
		"protocol":      ProtocolVersion,
		"heartbeat_sec": int(pingPeriod.Seconds()),
	}))
}

// ---------------------------------------------------------------------------
// Pumps
// ---------------------------------------------------------------------------

func (c *Conn) readPump() {
	defer func() {
		c.hub.unregister(c)
		c.cancel()
		_ = c.ws.Close()
		c.hub.log.Info("ws disconnected", "user", c.UserID.String(), "conn", c.ID)
	}()

	c.ws.SetReadLimit(128 * 1024)
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			c.deliver(serverEnvelope("connection.error", map[string]any{
				"error": "invalid JSON envelope",
			}))
			continue
		}
		if err := c.handle(env); err != nil {
			var apiErr *api.APIError
			code, msg := "internal_error", "Something went wrong."
			if errors.As(err, &apiErr) {
				code, msg = apiErr.Code, apiErr.Message
			}
			c.hub.log.Warn("ws op failed", "type", env.Type, "err", err.Error())
			switch env.Type {
			case "message.send":
				var data struct {
					ClientMessageID string `json:"client_message_id"`
				}
				_ = json.Unmarshal(env.Data, &data)
				c.deliver(serverEnvelope("message.failed", map[string]any{
					"client_message_id": data.ClientMessageID,
					"error":             map[string]string{"code": code, "message": msg},
				}))
			default:
				c.deliver(serverEnvelope("connection.error", map[string]any{
					"error": msg, "code": code, "in_response_to": env.Type,
				}))
			}
		}
	}
}

func (c *Conn) handle(env Envelope) error {
	switch env.Type {
	case "ping":
		c.deliver(serverEnvelope("pong", map[string]any{
			"client_event_id": env.EventID,
		}))
		return nil

	case "typing.start", "typing.stop":
		var data struct {
			ConversationID string `json:"conversation_id"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return api.ErrBadRequest
		}
		convID, err := uuid.Parse(data.ConversationID)
		if err != nil {
			return api.ErrBadRequest
		}
		member, err := c.hub.isMember(c.ctx, convID, c.UserID)
		if err != nil || !member {
			return api.ErrForbidden
		}
		// Fanout to other members only; ephemeral state in Redis.
		if env.Type == "typing.start" {
			_ = c.hub.presence.StartTyping(c.ctx, convID, c.UserID)
		} else {
			_ = c.hub.presence.StopTyping(c.ctx, convID, c.UserID)
		}
		members, _ := c.hub.membersExcluding(c.ctx, convID, c.UserID)
		c.hub.ToUsers(members, serverEnvelope(env.Type, map[string]any{
			"conversation_id": convID.String(),
			"user_id":         c.UserID.String(),
		}), "")
		return nil

	case "receipt.deliver":
		var data struct {
			ConversationID string `json:"conversation_id"`
			MaxSeq         int64  `json:"max_seq"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return api.ErrBadRequest
		}
		convID, err := uuid.Parse(data.ConversationID)
		if err != nil {
			return api.ErrBadRequest
		}
		member, err := c.hub.isMember(c.ctx, convID, c.UserID)
		if err != nil || !member {
			return api.ErrForbidden
		}
		return c.hub.receipts.MarkDelivered(c.ctx, convID, c.UserID, data.MaxSeq)

	case "receipt.read":
		var data struct {
			ConversationID string `json:"conversation_id"`
			MaxSeq         int64  `json:"max_seq"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return api.ErrBadRequest
		}
		convID, err := uuid.Parse(data.ConversationID)
		if err != nil {
			return api.ErrBadRequest
		}
		member, err := c.hub.isMember(c.ctx, convID, c.UserID)
		if err != nil || !member {
			return api.ErrForbidden
		}
		return c.hub.receipts.MarkRead(c.ctx, convID, c.UserID, data.MaxSeq)

	case "message.send":
		var req messages.SendRequest
		if err := json.Unmarshal(env.Data, &req); err != nil {
			return api.ErrBadRequest
		}
		convID, err := uuid.Parse(req.ConversationID)
		if err != nil {
			return api.ErrBadRequest
		}
		msg, err := c.hub.messages.Send(c.ctx, convID, c.UserID, req, c.ID)
		if err != nil {
			return err
		}
		// Ack the originating connection with the persisted message.
		c.deliver(serverEnvelope("message.sent", map[string]any{
			"client_message_id": req.ClientMessageID,
			"message":           msg,
		}))
		return nil

	default:
		return api.Errf(http.StatusBadRequest, "unknown_event", "Unknown event type.", nil)
	}
}

func (c *Conn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.ws.Close()
	}()
	for {
		select {
		case <-c.ctx.Done():
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			_ = c.ws.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"))
			return
		case env := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteJSON(env); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Internal DB helpers
// ---------------------------------------------------------------------------

func (h *Hub) isMember(ctx context.Context, convID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=$1 AND user_id=$2 AND left_at IS NULL)
	`, convID, userID).Scan(&ok)
	return ok, err
}

func (h *Hub) membersExcluding(ctx context.Context, convID, exclude uuid.UUID) ([]uuid.UUID, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT user_id FROM conversation_members
		WHERE conversation_id = $1 AND left_at IS NULL AND user_id <> $2
	`, convID, exclude)
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

// Shutdown closes every live connection gracefully.
func (h *Hub) Shutdown() {
	h.cancel()
	h.mu.RLock()
	var conns []*Conn
	for _, m := range h.conns {
		for _, c := range m {
			conns = append(conns, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range conns {
		c.deliver(serverEnvelope("connection.error", map[string]any{"error": "server restarting"}))
	}
	time.Sleep(500 * time.Millisecond)
}

// Healthy reports whether the hub can reach its dependencies.
func (h *Hub) Healthy(ctx context.Context) error {
	if err := h.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	if err := h.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	return nil
}
