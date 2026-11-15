// Package audit records security-relevant account events without ever storing
// secrets, message content or tokens.
package audit

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Actions recorded in the audit trail.
const (
	ActionRegister        = "auth.register"
	ActionLogin           = "auth.login"
	ActionLoginFailed     = "auth.login_failed"
	ActionLoginUnverified = "auth.login_unverified"
	ActionLogout          = "auth.logout"
	ActionEmailVerified   = "auth.email_verified"
	ActionPasswordReset   = "auth.password_reset"
	ActionPasswordChanged = "auth.password_changed"
	ActionSessionRevoked  = "auth.session_revoked"
	ActionSessionsRevoked = "auth.sessions_revoked_all"
	ActionProfileUpdated  = "profile.updated"
	ActionBlockedUser     = "contact.blocked"
	ActionUnblockedUser   = "contact.unblocked"
)

// Logger writes audit rows. Failures are logged but never break requests.
type Logger struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// New creates an audit logger.
func New(pool *pgxpool.Pool, log *slog.Logger) *Logger {
	return &Logger{pool: pool, log: log}
}

// Record writes one audit event. metadata must be JSON-safe and secret-free.
func (a *Logger) Record(ctx context.Context, action string, userID *string, ip, userAgent string, metadata map[string]any) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	_, err := a.pool.Exec(ctx, `
		INSERT INTO audit_logs (user_id, action, ip_address, user_agent, metadata)
		VALUES (NULLIF($1,'')::uuid, $2, NULLIF($3,''), NULLIF($4,''), $5)
	`, deref(userID), action, ip, userAgent, metadata)
	if err != nil {
		a.log.Warn("audit write failed", "action", action, "err", err.Error())
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
