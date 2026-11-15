// Package auth implements account registration, Argon2id password hashing,
// server-side sessions, email verification, password recovery and multi-device
// session management.
package auth

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singhtanishq/veylo/services/api/internal/api"
	"github.com/singhtanishq/veylo/services/api/internal/audit"
	"github.com/singhtanishq/veylo/services/api/internal/platform/config"
	"github.com/singhtanishq/veylo/services/api/internal/platform/mailer"
	"github.com/singhtanishq/veylo/services/api/internal/platform/passwords"
	"github.com/singhtanishq/veylo/services/api/internal/platform/ratelimit"
	"github.com/singhtanishq/veylo/services/api/internal/platform/tokens"
)

// ---------------------------------------------------------------------------
// Constants and helpers
// ---------------------------------------------------------------------------

const (
	sessionCookie = "veylo_session"
	sessionTTL    = 30 * 24 * time.Hour
)

var usernameRe = regexp.MustCompile(`^[a-z0-9_]{3,24}$`)
var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// NormalizeEmail lowercases and trims an email address.
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// NormalizeUsername lowercases an username.
func NormalizeUsername(u string) string { return strings.ToLower(strings.TrimSpace(u)) }

// ValidUsername reports whether the normalized username is acceptable.
func ValidUsername(u string) bool { return usernameRe.MatchString(u) }

// ValidPassword enforces a minimum strength policy (8+ chars, mixed classes).
func ValidPassword(p string) bool {
	if len(p) < 8 || len(p) > 128 {
		return false
	}
	var hasUpper, hasLower, hasDigit bool
	for _, r := range p {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	return hasUpper && hasLower && hasDigit
}

func isEmail(s string) bool { return emailRe.MatchString(s) }

// MailSender is the email-sending surface auth needs (implemented by the
// Mailpit-backed mailer; stubbed in tests).
type MailSender interface {
	Send(msg mailer.Message) error
}

// Handler exposes auth endpoints.
type Handler struct {
	pool   *pgxpool.Pool
	cfg    *config.Config
	mail   MailSender
	audit  *audit.Logger
	limits *ratelimit.Limiter
}

// NewHandler constructs the auth handler.
func NewHandler(pool *pgxpool.Pool, cfg *config.Config, mail MailSender, a *audit.Logger, l *ratelimit.Limiter) *Handler {
	return &Handler{pool: pool, cfg: cfg, mail: mail, audit: a, limits: l}
}

// ---------------------------------------------------------------------------
// DTOs
// ---------------------------------------------------------------------------

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Identifier string `json:"identifier"` // email or username
	Password   string `json:"password"`
}

type verifyEmailRequest struct {
	Token string `json:"token"`
}

type resendRequest struct {
	Email string `json:"email"`
}

type forgotRequest struct {
	Email string `json:"email"`
}

type resetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type userResponse struct {
	ID            string  `json:"id"`
	Username      string  `json:"username"`
	UsernameDisp  string  `json:"username_display"`
	Email         string  `json:"email"`
	EmailVerified bool    `json:"email_verified"`
	DisplayName   string  `json:"display_name"`
	About         *string `json:"about"`
	AvatarURL     *string `json:"avatar_url"`
	Status        string  `json:"account_status"`
	CreatedAt     string  `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

// Routes registers auth endpoints on the given router.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/auth/register", h.register)
	r.Post("/auth/login", h.login)
	r.Post("/auth/verify-email", h.verifyEmail)
	r.Post("/auth/resend-verification", h.resendVerification)
	r.Post("/auth/forgot-password", h.forgotPassword)
	r.Post("/auth/reset-password", h.resetPassword)

	r.Group(func(r chi.Router) {
		r.Use(h.RequireAuth)
		r.Post("/auth/logout", h.logout)
		r.Post("/auth/logout-all", h.logoutAll)
		r.Get("/auth/me", h.me)
		r.Post("/auth/change-password", h.changePassword)
	})

	// Sessions management (multi-device).
	r.Group(func(r chi.Router) {
		r.Use(h.RequireAuth)
		r.Get("/sessions", h.listSessions)
		r.Delete("/sessions/{sessionID}", h.revokeSession)
		r.Post("/sessions/revoke-others", h.revokeOthers)
	})
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	ip := api.ClientIP(r)
	if !h.limits.Allow("register:"+ip, ratelimit.PolicyRegister.PerSec, ratelimit.PolicyRegister.Burst) {
		api.WriteError(w, r, api.ErrRateLimited)
		return
	}

	var req registerRequest
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}

	type fieldErr struct{ Field, Message string }
	var details []fieldErr

	email := NormalizeEmail(req.Email)
	username := NormalizeUsername(req.Username)
	display := strings.TrimSpace(req.DisplayName)

	if !isEmail(email) || len(email) > 254 {
		details = append(details, fieldErr{"email", "Enter a valid email address."})
	}
	if !ValidUsername(username) {
		details = append(details, fieldErr{"username", "Usernames are 3-24 characters: lowercase letters, numbers and underscores."})
	}
	if !ValidPassword(req.Password) {
		details = append(details, fieldErr{"password", "Passwords need at least 8 characters with upper, lower and a number."})
	}
	if len(display) < 1 || len([]rune(display)) > 80 {
		details = append(details, fieldErr{"display_name", "Display name is required (max 80 characters)."})
	}
	if len(details) > 0 {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Please check the highlighted fields.", details))
		return
	}

	hash, err := passwords.Hash(req.Password)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(ctx)

	var userID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO users (username, username_display, email, password_hash, account_status)
		VALUES ($1, $2, $3, $4, 'pending_verification')
		RETURNING id
	`, username, display, email, hash).Scan(&userID)
	if err != nil {
		var pgErr interface{ Error() string }
		if errors.As(err, &pgErr) && strings.Contains(err.Error(), "users_email_format") {
			api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation, "Invalid email.", nil))
			return
		}
		if strings.Contains(err.Error(), "users_username_key") || strings.Contains(err.Error(), "_username_key") {
			api.WriteError(w, r, api.Errf(http.StatusConflict, api.CodeUsernameExists,
				"That username is already taken.", nil))
			return
		}
		if strings.Contains(err.Error(), "users_email_key") || strings.Contains(err.Error(), "_email_key") {
			api.WriteError(w, r, api.Errf(http.StatusConflict, api.CodeEmailExists,
				"An account with this email already exists.", nil))
			return
		}
		api.WriteError(w, r, err)
		return
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO user_profiles (user_id, display_name) VALUES ($1, $2)
	`, userID, display); err != nil {
		api.WriteError(w, r, err)
		return
	}
	for _, s := range []string{
		`INSERT INTO user_preferences (user_id) VALUES ($1)`,
		`INSERT INTO user_privacy_settings (user_id) VALUES ($1)`,
	} {
		if _, err := tx.Exec(ctx, s, userID); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}

	rawToken, tokenHash := tokens.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO email_verification_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, now() + make_interval(mins => $3))
	`, userID, tokenHash, h.cfg.VerificationTTLMin); err != nil {
		api.WriteError(w, r, err)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		api.WriteError(w, r, err)
		return
	}

	uid := userID.String()
	ua := r.UserAgent()
	h.audit.Record(context.Background(), audit.ActionRegister, &uid, ip, ua, nil)

	// Send the verification email via local SMTP (Mailpit in development).
	verifyURL := fmt.Sprintf("%s/verify-email?token=%s", h.cfg.AppBaseURL, rawToken)
	msg := mailer.EmailTemplate(
		"Verify your email",
		fmt.Sprintf("Welcome to Veylo, %s! Confirm your email address to activate your account. This link expires in %d hours.", html.EscapeString(display), h.cfg.VerificationTTLMin/60),
		verifyURL, "Verify email", h.cfg.MailpitWebURL)
	msg.To = email
	msg.Subject = "Verify your Veylo email"
	if err := h.mail.Send(msg); err != nil {
		// The account exists; surface a retryable hint without failing silently.
		api.WriteError(w, r, api.Errf(http.StatusInternalServerError, api.CodeInternal,
			"Account created but the verification email could not be sent. Use resend from the login screen.", nil))
		return
	}

	api.WriteJSON(w, r, http.StatusCreated, map[string]any{
		"user_id":     userID,
		"email":       email,
		"next":        "verify_email",
		"mailpit_url": h.cfg.MailpitWebURL,
	})
}

// ---------------------------------------------------------------------------
// Email verification
// ---------------------------------------------------------------------------

func (h *Handler) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		api.WriteError(w, r, api.ErrValidation)
		return
	}

	tokenHash := tokens.Hash(req.Token)
	ctx := r.Context()

	var userID uuid.UUID
	var expires time.Time
	var used *time.Time
	err := h.pool.QueryRow(ctx, `
		SELECT user_id, expires_at, used_at FROM email_verification_tokens WHERE token_hash = $1
	`, tokenHash).Scan(&userID, &expires, &used)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.WriteError(w, r, api.Errf(http.StatusBadRequest, api.CodeBadRequest,
				"This verification link is invalid.", nil))
			return
		}
		api.WriteError(w, r, err)
		return
	}
	if used != nil {
		api.WriteError(w, r, api.Errf(http.StatusBadRequest, api.CodeBadRequest,
			"This verification link was already used.", nil))
		return
	}
	if time.Now().After(expires) {
		api.WriteError(w, r, api.Errf(http.StatusBadRequest, api.CodeBadRequest,
			"This verification link has expired. Request a new one.", nil))
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE users
		SET email_verified_at = now(),
		    account_status = CASE WHEN account_status = 'pending_verification' THEN 'active' ELSE account_status END,
		    updated_at = now()
		WHERE id = $1 AND email_verified_at IS NULL
	`, userID); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if _, err := tx.Exec(ctx, `
		UPDATE email_verification_tokens SET used_at = now() WHERE token_hash = $1
	`, tokenHash); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		api.WriteError(w, r, err)
		return
	}

	uid := userID.String()
	h.audit.Record(context.Background(), audit.ActionEmailVerified, &uid, api.ClientIP(r), r.UserAgent(), nil)
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"verified": true, "next": "login"})
}

func (h *Handler) resendVerification(w http.ResponseWriter, r *http.Request) {
	ip := api.ClientIP(r)
	if !h.limits.Allow("verify:"+ip, ratelimit.PolicyVerify.PerSec, ratelimit.PolicyVerify.Burst) {
		api.WriteError(w, r, api.ErrRateLimited)
		return
	}

	var req resendRequest
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}

	// If authenticated, resend for the current user; otherwise by email.
	var userID uuid.UUID
	var email, display string
	if api.UserID(r.Context()) != uuid.Nil {
		userID = api.UserID(r.Context())
		err := h.pool.QueryRow(r.Context(), `
			SELECT u.email, p.display_name FROM users u
			JOIN user_profiles p ON p.user_id = u.id WHERE u.id = $1
		`, userID).Scan(&email, &display)
		if err != nil {
			api.WriteError(w, r, err)
			return
		}
	} else {
		email = NormalizeEmail(req.Email)
		if !isEmail(email) {
			api.WriteError(w, r, api.ErrValidation)
			return
		}
		err := h.pool.QueryRow(r.Context(), `
			SELECT u.id, u.email, p.display_name FROM users u
			JOIN user_profiles p ON p.user_id = u.id
			WHERE u.email = $1 AND u.email_verified_at IS NULL AND u.account_status = 'pending_verification'
		`, email).Scan(&userID, &email, &display)
		if err != nil {
			// Anti-enumeration: identical response whether or not the account exists.
			api.WriteJSON(w, r, http.StatusOK, map[string]any{"sent": true})
			return
		}
	}

	rawToken, tokenHash := tokens.New()
	if _, err := h.pool.Exec(r.Context(), `
		INSERT INTO email_verification_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, now() + make_interval(mins => $3))
	`, userID, tokenHash, h.cfg.VerificationTTLMin); err != nil {
		api.WriteError(w, r, err)
		return
	}

	verifyURL := fmt.Sprintf("%s/verify-email?token=%s", h.cfg.AppBaseURL, rawToken)
	msg := mailer.EmailTemplate(
		"Verify your email",
		"Here is a fresh verification link for your Veylo account.",
		verifyURL, "Verify email", h.cfg.MailpitWebURL)
	msg.To = email
	msg.Subject = "Your new Veylo verification link"
	if err := h.mail.Send(msg); err != nil {
		api.WriteError(w, r, api.Errf(http.StatusInternalServerError, api.CodeInternal,
			"Could not send the email. Is Mailpit running?", nil))
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"sent": true})
}

// ---------------------------------------------------------------------------
// Login / logout
// ---------------------------------------------------------------------------

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	ip := api.ClientIP(r)
	var req loginRequest
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" || req.Password == "" {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Enter your email/username and password.", nil))
		return
	}
	// Rate limit by IP and by identifier to slow brute force on both axes.
	if !h.limits.Allow("login:ip:"+ip, ratelimit.PolicyLogin.PerSec, ratelimit.PolicyLogin.Burst) ||
		!h.limits.Allow("login:id:"+NormalizeEmail(identifier), ratelimit.PolicyLogin.PerSec, ratelimit.PolicyLogin.Burst) {
		api.WriteError(w, r, api.ErrRateLimited)
		return
	}

	ctx := r.Context()
	var (
		userID   uuid.UUID
		username string
		email    string
		hash     string
		status   string
		verified *time.Time
		display  string
	)
	err := h.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.email, u.password_hash, u.account_status, u.email_verified_at, p.display_name
		FROM users u JOIN user_profiles p ON p.user_id = u.id
		WHERE u.email = $1 OR u.username = $2
	`, NormalizeEmail(identifier), NormalizeUsername(identifier)).
		Scan(&userID, &username, &email, &hash, &status, &verified, &display)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.audit.Record(context.Background(), audit.ActionLoginFailed, nil, ip, r.UserAgent(),
				map[string]any{"reason": "unknown_identifier"})
			// Constant-ish work to damp user enumeration timing.
			_, _ = passwords.Hash(req.Password)
			api.WriteError(w, r, api.Errf(http.StatusUnauthorized, api.CodeUnauthorized,
				"Incorrect email/username or password.", nil))
			return
		}
		api.WriteError(w, r, err)
		return
	}

	ok, err := passwords.Verify(req.Password, hash)
	if err != nil || !ok {
		uid := userID.String()
		h.audit.Record(context.Background(), audit.ActionLoginFailed, &uid, ip, r.UserAgent(),
			map[string]any{"reason": "bad_password"})
		api.WriteError(w, r, api.Errf(http.StatusUnauthorized, api.CodeUnauthorized,
			"Incorrect email/username or password.", nil))
		return
	}

	if status == "pending_verification" || verified == nil {
		uid := userID.String()
		h.audit.Record(context.Background(), audit.ActionLoginUnverified, &uid, ip, r.UserAgent(), nil)
		api.WriteError(w, r, api.Errf(http.StatusForbidden, api.CodeEmailNotVerified,
			"Your email is not verified yet. Check Mailpit for the verification link.", nil))
		return
	}
	if status != "active" {
		api.WriteError(w, r, api.Errf(http.StatusForbidden, api.CodeForbidden,
			"This account is not available.", nil))
		return
	}

	session, rawToken, err := h.createSession(ctx, userID, r)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}

	uid := userID.String()
	h.audit.Record(context.Background(), audit.ActionLogin, &uid, ip, r.UserAgent(),
		map[string]any{"session_id": session.ID.String()})
	h.setSessionCookie(w, rawToken, session.ExpiresAt)

	api.WriteJSON(w, r, http.StatusOK, map[string]any{
		"user":    h.loadUserResponse(ctx, userID),
		"session": session,
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	sessionID := api.SessionID(r.Context())
	if sessionID != uuid.Nil {
		if _, err := h.pool.Exec(r.Context(),
			`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, sessionID); err == nil {
			uid := api.UserID(r.Context()).String()
			h.audit.Record(context.Background(), audit.ActionLogout, &uid, api.ClientIP(r), r.UserAgent(), nil)
		}
	}
	h.setSessionCookie(w, "", time.Now().Add(-1*time.Hour))
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"logged_out": true})
}

func (h *Handler) logoutAll(w http.ResponseWriter, r *http.Request) {
	userID := api.UserID(r.Context())
	if _, err := h.pool.Exec(r.Context(),
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
		api.WriteError(w, r, err)
		return
	}
	uid := userID.String()
	h.audit.Record(context.Background(), audit.ActionSessionsRevoked, &uid, api.ClientIP(r), r.UserAgent(), nil)
	h.setSessionCookie(w, "", time.Now().Add(-1*time.Hour))
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"logged_out": true})
}

// ---------------------------------------------------------------------------
// Sessions (multi-device)
// ---------------------------------------------------------------------------

type sessionResponse struct {
	ID           string `json:"id"`
	DeviceLabel  string `json:"device_label"`
	DeviceType   string `json:"device_type"`
	IPAddress    string `json:"ip_address"`
	CreatedAt    string `json:"created_at"`
	LastActiveAt string `json:"last_active_at"`
	Current      bool   `json:"current"`
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, device_label, device_type, COALESCE(ip_address,''), created_at, last_active_at
		FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY last_active_at DESC
	`, api.UserID(r.Context()))
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	out := []sessionResponse{}
	current := api.SessionID(r.Context())
	for rows.Next() {
		var s sessionResponse
		var id uuid.UUID
		var created, active time.Time
		if err := rows.Scan(&id, &s.DeviceLabel, &s.DeviceType, &s.IPAddress, &created, &active); err != nil {
			api.WriteError(w, r, err)
			return
		}
		s.ID = id.String()
		s.CreatedAt = created.UTC().Format(time.RFC3339)
		s.LastActiveAt = active.UTC().Format(time.RFC3339)
		s.Current = id == current
		out = append(out, s)
	}
	api.WriteJSON(w, r, http.StatusOK, out)
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	userID := api.UserID(r.Context())
	tag, err := h.pool.Exec(r.Context(),
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		api.WriteError(w, r, api.ErrNotFound)
		return
	}
	uid := userID.String()
	h.audit.Record(context.Background(), audit.ActionSessionRevoked, &uid, api.ClientIP(r), r.UserAgent(),
		map[string]any{"revoked_session": id.String()})
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"revoked": true})
}

func (h *Handler) revokeOthers(w http.ResponseWriter, r *http.Request) {
	userID := api.UserID(r.Context())
	tag, err := h.pool.Exec(r.Context(), `
		UPDATE sessions SET revoked_at = now()
		WHERE user_id = $1 AND revoked_at IS NULL AND id <> $2
	`, userID, api.SessionID(r.Context()))
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	uid := userID.String()
	h.audit.Record(context.Background(), audit.ActionSessionsRevoked, &uid, api.ClientIP(r), r.UserAgent(),
		map[string]any{"count": tag.RowsAffected()})
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"revoked": tag.RowsAffected()})
}

// ---------------------------------------------------------------------------
// Password management
// ---------------------------------------------------------------------------

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	ip := api.ClientIP(r)
	if !h.limits.Allow("reset:"+ip, ratelimit.PolicyReset.PerSec, ratelimit.PolicyReset.Burst) {
		api.WriteError(w, r, api.ErrRateLimited)
		return
	}
	var req forgotRequest
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	email := NormalizeEmail(req.Email)

	var userID uuid.UUID
	var displayName string
	err := h.pool.QueryRow(r.Context(), `
		SELECT u.id, p.display_name FROM users u
		JOIN user_profiles p ON p.user_id = u.id
		WHERE u.email = $1 AND u.account_status = 'active'
	`, email).Scan(&userID, &displayName)
	if err != nil {
		// Anti-enumeration: identical generic response.
		api.WriteJSON(w, r, http.StatusOK, map[string]any{"sent": true})
		return
	}

	rawToken, tokenHash := tokens.New()
	if _, err := h.pool.Exec(r.Context(), `
		INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, now() + make_interval(mins => $3))
	`, userID, tokenHash, h.cfg.ResetTTLMin); err != nil {
		api.WriteError(w, r, err)
		return
	}

	resetURL := fmt.Sprintf("%s/reset-password?token=%s", h.cfg.AppBaseURL, rawToken)
	msg := mailer.EmailTemplate(
		"Reset your password",
		"We received a request to reset your Veylo password. If this was you, use the link below. It expires in 1 hour and can be used once.",
		resetURL, "Reset password", h.cfg.MailpitWebURL)
	msg.To = email
	msg.Subject = "Reset your Veylo password"
	if err := h.mail.Send(msg); err != nil {
		api.WriteError(w, r, api.Errf(http.StatusInternalServerError, api.CodeInternal,
			"Could not send the email. Is Mailpit running?", nil))
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"sent": true})
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetRequest
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if !ValidPassword(req.Password) {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Passwords need at least 8 characters with upper, lower and a number.", nil))
		return
	}
	tokenHash := tokens.Hash(strings.TrimSpace(req.Token))
	ctx := r.Context()

	var userID uuid.UUID
	var expires time.Time
	var used *time.Time
	err := h.pool.QueryRow(ctx, `
		SELECT user_id, expires_at, used_at FROM password_reset_tokens WHERE token_hash = $1
	`, tokenHash).Scan(&userID, &expires, &used)
	if err != nil || used != nil || time.Now().After(expires) {
		api.WriteError(w, r, api.Errf(http.StatusBadRequest, api.CodeBadRequest,
			"This reset link is invalid, expired or already used.", nil))
		return
	}

	hash, err := passwords.Hash(req.Password)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2`, hash, userID); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE password_reset_tokens SET used_at = now() WHERE token_hash = $1`, tokenHash); err != nil {
		api.WriteError(w, r, err)
		return
	}
	// Invalidate every existing session after a reset.
	if _, err := tx.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		api.WriteError(w, r, err)
		return
	}

	uid := userID.String()
	h.audit.Record(context.Background(), audit.ActionPasswordReset, &uid, api.ClientIP(r), r.UserAgent(), nil)
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"reset": true})
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if !ValidPassword(req.NewPassword) {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Passwords need at least 8 characters with upper, lower and a number.", nil))
		return
	}
	ctx := r.Context()
	userID := api.UserID(r.Context())

	var hash string
	if err := h.pool.QueryRow(ctx,
		`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash); err != nil {
		api.WriteError(w, r, err)
		return
	}
	ok, err := passwords.Verify(req.CurrentPassword, hash)
	if err != nil || !ok {
		api.WriteError(w, r, api.Errf(http.StatusUnauthorized, api.CodeUnauthorized,
			"Your current password is incorrect.", nil))
		return
	}
	newHash, err := passwords.Hash(req.NewPassword)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	if _, err := h.pool.Exec(ctx,
		`UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2`, newHash, userID); err != nil {
		api.WriteError(w, r, err)
		return
	}
	uid := userID.String()
	h.audit.Record(context.Background(), audit.ActionPasswordChanged, &uid, api.ClientIP(r), r.UserAgent(), nil)
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"changed": true})
}

// ---------------------------------------------------------------------------
// Current user
// ---------------------------------------------------------------------------

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	user := h.loadUserResponse(r.Context(), api.UserID(r.Context()))
	api.WriteJSON(w, r, http.StatusOK, user)
}

func (h *Handler) loadUserResponse(ctx context.Context, userID uuid.UUID) userResponse {
	var u userResponse
	var emailVerified *time.Time
	var avatarID *uuid.UUID
	var about *string
	var createdAt time.Time
	_ = h.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.username_display, u.email, u.email_verified_at, u.account_status, u.created_at,
		       p.display_name, p.about, p.avatar_attachment_id
		FROM users u JOIN user_profiles p ON p.user_id = u.id
		WHERE u.id = $1
	`, userID).Scan(&u.ID, &u.Username, &u.UsernameDisp, &u.Email, &emailVerified, &u.Status, &createdAt,
		&u.DisplayName, &about, &avatarID)
	u.EmailVerified = emailVerified != nil
	u.About = about
	if avatarID != nil {
		url := avatarURL(*avatarID)
		u.AvatarURL = &url
	}
	u.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return u
}

func avatarURL(id uuid.UUID) string { return "/v1/attachments/" + id.String() + "/download" }

// ---------------------------------------------------------------------------
// Session creation and cookie / middleware plumbing
// ---------------------------------------------------------------------------

type sessionInfo struct {
	ID          uuid.UUID
	DeviceLabel string
	DeviceType  string
	ExpiresAt   time.Time
}

func (h *Handler) createSession(ctx context.Context, userID uuid.UUID, r *http.Request) (sessionInfo, string, error) {
	rawToken, tokenHash := tokens.New()
	label, dtype := classifyDevice(r.UserAgent())
	now := time.Now()
	var s sessionInfo
	err := h.pool.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, device_label, device_type, user_agent, ip_address, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, device_label, device_type, expires_at
	`, userID, tokenHash, label, dtype, truncate(r.UserAgent(), 400), api.ClientIP(r),
		now.Add(sessionTTL)).Scan(&s.ID, &s.DeviceLabel, &s.DeviceType, &s.ExpiresAt)
	if err != nil {
		return s, "", err
	}
	return s, rawToken, nil
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, rawToken string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   !h.cfg.IsDevelopment(), // localhost dev runs plain HTTP
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
}

// RequireAuth resolves the session cookie into user+session context.
func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			api.WriteError(w, r, api.ErrUnauthorized)
			return
		}
		tokenHash := tokens.Hash(c.Value)
		var (
			sessionID, userID uuid.UUID
			expires, lastAct  time.Time
		)
		err = h.pool.QueryRow(r.Context(), `
			SELECT id, user_id, expires_at, last_active_at FROM sessions
			WHERE token_hash = $1 AND revoked_at IS NULL
		`, tokenHash).Scan(&sessionID, &userID, &expires, &lastAct)
		if err != nil {
			api.WriteError(w, r, api.ErrUnauthorized)
			return
		}
		if time.Now().After(expires) {
			api.WriteError(w, r, api.ErrUnauthorized)
			return
		}
		// Sliding renewal: refresh activity + expiry when stale.
		if time.Since(lastAct) > time.Duration(h.cfg.SessionRenewMin)*time.Minute {
			_, _ = h.pool.Exec(r.Context(),
				`UPDATE sessions SET last_active_at = now(), expires_at = $2 WHERE id = $1`,
				sessionID, time.Now().Add(sessionTTL))
		}
		ctx := api.SetUserID(r.Context(), userID)
		ctx = api.SetSessionID(ctx, sessionID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---------------------------------------------------------------------------
// Device classification (no fingerprinting; UA-string buckets only)
// ---------------------------------------------------------------------------

func classifyDevice(ua string) (label, dtype string) {
	s := strings.ToLower(ua)
	device := "Web browser"
	dtype = "web"
	switch {
	case strings.Contains(s, "iphone"), strings.Contains(s, "ipad"), strings.Contains(s, "android"),
		strings.Contains(s, "mobile"):
		device, dtype = "Mobile browser", "mobile"
	case strings.Contains(s, "electron"):
		device, dtype = "Desktop app", "desktop"
	}
	browser := "Web browser"
	switch {
	case strings.Contains(s, "edg/"):
		browser = "Edge"
	case strings.Contains(s, "chrome/") && !strings.Contains(s, "chromium"):
		browser = "Chrome"
	case strings.Contains(s, "firefox"):
		browser = "Firefox"
	case strings.Contains(s, "safari") && !strings.Contains(s, "chrome"):
		browser = "Safari"
	}
	if dtype == "web" && browser != "Web browser" {
		device = browser
	}
	return truncate(device, 120), dtype
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
