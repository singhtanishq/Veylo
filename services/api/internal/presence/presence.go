// Package presence tracks online state and typing indicators in Redis.
// Presence is derived from active authenticated WebSocket connections and is
// ephemeral by design — it is never persisted in PostgreSQL. Last-seen
// timestamps are stored on users.last_seen_at when the final connection drops.
package presence

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/singhtanishq/veylo/services/api/internal/platform/redisx"
)

// Store manages presence and typing state in Redis.
type Store struct {
	rdb *redis.Client
}

// NewStore builds a presence store.
func NewStore(rdb *redis.Client) *Store { return &Store{rdb: rdb} }

// SetOnline marks a user online (idempotent) with a TTL safety net. The hub
// refreshes it while connections are alive.
func (s *Store) SetOnline(ctx context.Context, userID uuid.UUID) error {
	return s.rdb.SAdd(ctx, redisx.KeyPresenceOnline, userID.String()).Err()
}

// SetOffline removes a user from the online set.
func (s *Store) SetOffline(ctx context.Context, userID uuid.UUID) error {
	return s.rdb.SRem(ctx, redisx.KeyPresenceOnline, userID.String()).Err()
}

// IsOnline reports whether the user currently has a live connection.
func (s *Store) IsOnline(ctx context.Context, userID uuid.UUID) bool {
	return s.rdb.SIsMember(ctx, redisx.KeyPresenceOnline, userID.String()).Val()
}

// OnlineSet returns all currently-online user IDs.
func (s *Store) OnlineSet(ctx context.Context) (map[uuid.UUID]bool, error) {
	vals, err := s.rdb.SMembers(ctx, redisx.KeyPresenceOnline).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]bool, len(vals))
	for _, v := range vals {
		if id, err := uuid.Parse(v); err == nil {
			out[id] = true
		}
	}
	return out, nil
}

// StartTyping marks the user as typing in a conversation for 6 seconds.
func (s *Store) StartTyping(ctx context.Context, conversationID, userID uuid.UUID) error {
	return s.rdb.Set(ctx, redisx.KeyTyping+conversationID.String()+":"+userID.String(), "1", 6*time.Second).Err()
}

// StopTyping clears the typing mark.
func (s *Store) StopTyping(ctx context.Context, conversationID, userID uuid.UUID) error {
	return s.rdb.Del(ctx, redisx.KeyTyping+conversationID.String()+":"+userID.String()).Err()
}

// IsTyping reports whether the user is marked as typing in the conversation.
func (s *Store) IsTyping(ctx context.Context, conversationID, userID uuid.UUID) bool {
	n, err := s.rdb.Exists(ctx, redisx.KeyTyping+conversationID.String()+":"+userID.String()).Result()
	return err == nil && n > 0
}
