// Package redisx wraps the Redis client used for ephemeral presence, typing
// state and WebSocket connection tickets. Nothing here is a source of truth;
// PostgreSQL remains authoritative for all persistent data.
package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// New connects to Redis and verifies connectivity.
func New(ctx context.Context, url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redisx: parse url: %w", err)
	}
	client := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redisx: ping: %w", err)
	}
	return client, nil
}

// Keys used across services.
const (
	KeyPresenceOnline = "veylo:presence:online" // SET of user IDs currently connected
	KeyWSTicket       = "veylo:ws:ticket:"      // one-time WS auth tickets
	KeyTyping         = "veylo:typing:"         // typing:{conversation}:{user}
)
