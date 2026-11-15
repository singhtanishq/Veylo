// Package users implements profile management, username availability and
// privacy-respecting user discovery (username / email / verified phone).
package users

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singhtanishq/veylo/services/api/internal/api"
	"github.com/singhtanishq/veylo/services/api/internal/auth"
	"github.com/singhtanishq/veylo/services/api/internal/platform/ratelimit"
	"github.com/singhtanishq/veylo/services/api/internal/presence"
)

var e164Re = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// Handler exposes user endpoints.
type Handler struct {
	pool     *pgxpool.Pool
	limits   *ratelimit.Limiter
	presence *presence.Store
}

// NewHandler constructs the users handler.
func NewHandler(pool *pgxpool.Pool, l *ratelimit.Limiter, p *presence.Store) *Handler {
	return &Handler{pool: pool, limits: l, presence: p}
}

// PublicUser is the privacy-filtered profile shown to other users.
type PublicUser struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	About       *string `json:"about"`
	AvatarURL   *string `json:"avatar_url"`
	Online      bool    `json:"online"`
	LastSeenAt  *string `json:"last_seen_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
	IsContact   bool    `json:"is_contact"`
	BlockedByMe bool    `json:"blocked_by_me,omitempty"`
	BlockedMe   bool    `json:"-"`
}

// Routes registers user endpoints. Authentication middleware is applied by
// the router; every handler here requires an authenticated session.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/users/search", h.search)
	r.Get("/users/me", h.me)
	r.Patch("/users/me", h.updateMe)
	r.Post("/users/me/phone", h.setPhone)
	r.Get("/users/{userID}", h.getProfile)
}

// PublicRoutes registers endpoints available without a session (rate limited
// at the policy level). Username availability mirrors public identifier
// semantics: usernames are already discoverable via search.
func (h *Handler) PublicRoutes(r chi.Router) {
	r.Get("/users/username/availability", h.usernameAvailability)
}

// ---------------------------------------------------------------------------
// Current user profile
// ---------------------------------------------------------------------------

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := api.UserID(ctx)

	var email string
	var emailVerifiedAt *time.Time
	if err := h.pool.QueryRow(ctx,
		`SELECT email, email_verified_at FROM users WHERE id = $1`, uid).
		Scan(&email, &emailVerifiedAt); err != nil {
		api.WriteError(w, r, err)
		return
	}

	var displayName string
	var about *string
	var avatarID *uuid.UUID
	var createdAt time.Time
	if err := h.pool.QueryRow(ctx, `
		SELECT display_name, about, avatar_attachment_id, created_at
		FROM user_profiles WHERE user_id = $1
	`, uid).Scan(&displayName, &about, &avatarID, &createdAt); err != nil {
		api.WriteError(w, r, err)
		return
	}

	resp := map[string]any{
		"id":             uid,
		"username":       h.usernameOf(ctx, uid),
		"display_name":   displayName,
		"about":          about,
		"email":          email,
		"email_verified": emailVerifiedAt != nil,
		"created_at":     createdAt.UTC().Format(time.RFC3339),
	}
	if avatarID != nil {
		url := "/v1/attachments/" + avatarID.String() + "/download"
		resp["avatar_url"] = &url
	}
	// Phone info (own view only).
	var phone *string
	var phoneVerified bool
	if err := h.pool.QueryRow(ctx,
		`SELECT phone_e164, verified FROM user_phone_numbers WHERE user_id = $1`, uid).
		Scan(&phone, &phoneVerified); err == nil && phone != nil {
		resp["phone"] = map[string]any{"number": *phone, "verified": phoneVerified}
	}
	api.WriteJSON(w, r, http.StatusOK, resp)
}

func (h *Handler) usernameOf(ctx context.Context, uid uuid.UUID) string {
	var u string
	_ = h.pool.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, uid).Scan(&u)
	return u
}

type updateMeRequest struct {
	DisplayName *string `json:"display_name"`
	Username    *string `json:"username"`
	About       *string `json:"about"`
}

func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	var req updateMeRequest
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	userID := api.UserID(r.Context())

	if req.Username != nil {
		username := auth.NormalizeUsername(*req.Username)
		if !auth.ValidUsername(username) {
			api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
				"Usernames are 3-24 characters: lowercase letters, numbers and underscores.", nil))
			return
		}
		tag, err := h.pool.Exec(ctx,
			`UPDATE users SET username = $1, username_display = $2, updated_at = now() WHERE id = $3`,
			username, strings.TrimSpace(*req.Username), userID)
		if err != nil {
			if strings.Contains(err.Error(), "users_username_key") {
				api.WriteError(w, r, api.Errf(http.StatusConflict, api.CodeUsernameExists,
					"That username is already taken.", nil))
				return
			}
			api.WriteError(w, r, err)
			return
		}
		if tag.RowsAffected() == 0 {
			api.WriteError(w, r, api.ErrNotFound)
			return
		}
	}

	if req.DisplayName != nil {
		display := strings.TrimSpace(*req.DisplayName)
		if len([]rune(display)) < 1 || len([]rune(display)) > 80 {
			api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
				"Display name is required (max 80 characters).", nil))
			return
		}
		if _, err := h.pool.Exec(ctx,
			`UPDATE user_profiles SET display_name = $1, updated_at = now() WHERE user_id = $2`,
			display, userID); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}

	if req.About != nil {
		about := strings.TrimSpace(*req.About)
		if len([]rune(about)) > 280 {
			api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
				"About is limited to 280 characters.", nil))
			return
		}
		if _, err := h.pool.Exec(ctx,
			`UPDATE user_profiles SET about = NULLIF($1,''), updated_at = now() WHERE user_id = $2`,
			about, userID); err != nil {
			api.WriteError(w, r, err)
			return
		}
	}

	api.WriteJSON(w, r, http.StatusOK, map[string]any{"updated": true})
}

type setPhoneRequest struct {
	Phone string `json:"phone"`
}

func (h *Handler) setPhone(w http.ResponseWriter, r *http.Request) {
	var req setPhoneRequest
	if err := api.DecodeJSON(w, r, &req); err != nil {
		api.WriteError(w, r, err)
		return
	}
	phone := strings.TrimSpace(req.Phone)
	if phone == "" {
		// Remove number.
		if _, err := h.pool.Exec(r.Context(),
			`DELETE FROM user_phone_numbers WHERE user_id = $1`, api.UserID(r.Context())); err != nil {
			api.WriteError(w, r, err)
			return
		}
		api.WriteJSON(w, r, http.StatusOK, map[string]any{"phone": nil, "verified": false})
		return
	}
	if !e164Re.MatchString(phone) {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Phone must be in international format, e.g. +919876543210.", nil))
		return
	}
	// Local product: numbers are stored but remain unverified. Phone discovery
	// only ever matches verified numbers, so an unverified number is invisible
	// to search. A real SMS provider is a documented extension point.
	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO user_phone_numbers (user_id, phone_e164, verified)
		VALUES ($1, $2, FALSE)
		ON CONFLICT (user_id) DO UPDATE SET phone_e164 = EXCLUDED.phone_e164, verified = FALSE, verified_at = NULL, updated_at = now()
	`, api.UserID(r.Context()), phone)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"phone": phone, "verified": false,
		"note": "Phone verification requires an SMS provider and is disabled in the local build."})
}

// ---------------------------------------------------------------------------
// Discovery
// ---------------------------------------------------------------------------

func (h *Handler) usernameAvailability(w http.ResponseWriter, r *http.Request) {
	if !h.limits.Allow("avail:"+api.ClientIP(r), ratelimit.PolicySearch.PerSec, ratelimit.PolicySearch.Burst) {
		api.WriteError(w, r, api.ErrRateLimited)
		return
	}
	username := auth.NormalizeUsername(r.URL.Query().Get("username"))
	if !auth.ValidUsername(username) {
		api.WriteJSON(w, r, http.StatusOK, map[string]any{
			"available": false, "reason": "Usernames are 3-24 characters: lowercase letters, numbers and underscores.",
		})
		return
	}
	var exists bool
	if err := h.pool.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`, username).Scan(&exists); err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"available": !exists, "username": username})
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	me := api.UserID(r.Context())
	if !h.limits.Allow("search:"+me.String(), ratelimit.PolicySearch.PerSec, ratelimit.PolicySearch.Burst) {
		api.WriteError(w, r, api.ErrRateLimited)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 1 || len(q) > 100 {
		api.WriteError(w, r, api.Errf(http.StatusUnprocessableEntity, api.CodeValidation,
			"Search needs 1-100 characters.", nil))
		return
	}
	ctx := r.Context()

	// Interpret the query: @username | email | E.164 phone | fuzzy username/display.
	var candidates []uuid.UUID
	switch {
	case strings.HasPrefix(q, "@"):
		u := auth.NormalizeUsername(strings.TrimPrefix(q, "@"))
		rows, err := h.pool.Query(ctx, `
			SELECT id FROM users
			WHERE username LIKE $1 AND id <> $2 AND account_status = 'active'
			ORDER BY (username = $3) DESC, username LIMIT 20
		`, u+"%", me, u)
		if err != nil {
			api.WriteError(w, r, err)
			return
		}
		candidates = collectIDs(rows)
	case isEmailQuery(q):
		email := auth.NormalizeEmail(q)
		rows, err := h.pool.Query(ctx, `
			SELECT u.id FROM users u
			JOIN user_privacy_settings s ON s.user_id = u.id
			WHERE u.email = $1 AND u.id <> $2 AND u.account_status = 'active'
			  AND u.email_verified_at IS NOT NULL AND s.discoverable_by_email = TRUE
			LIMIT 20
		`, email, me)
		if err != nil {
			api.WriteError(w, r, err)
			return
		}
		candidates = collectIDs(rows)
	case e164Re.MatchString(q):
		// Phone discovery only for verified numbers with discoverability on.
		rows, err := h.pool.Query(ctx, `
			SELECT u.id FROM users u
			JOIN user_phone_numbers ph ON ph.user_id = u.id
			JOIN user_privacy_settings s ON s.user_id = u.id
			WHERE ph.phone_e164 = $1 AND ph.verified = TRUE
			  AND u.id <> $2 AND u.account_status = 'active' AND s.discoverable_by_phone = TRUE
			LIMIT 20
		`, q, me)
		if err != nil {
			api.WriteError(w, r, err)
			return
		}
		candidates = collectIDs(rows)
	default:
		pat := "%" + escapeLike(strings.ToLower(q)) + "%"
		rows, err := h.pool.Query(ctx, `
			SELECT u.id FROM users u
			JOIN user_profiles p ON p.user_id = u.id
			WHERE u.account_status = 'active' AND u.id <> $1
			  AND (u.username LIKE $2 OR p.display_name ILIKE $3)
			GROUP BY u.id, u.username
			ORDER BY u.username LIMIT 20
		`, me, pat, "%"+escapeLike(q)+"%")
		if err != nil {
			api.WriteError(w, r, err)
			return
		}
		candidates = collectIDs(rows)
	}

	results := []PublicUser{}
	for _, id := range candidates {
		u, err := h.publicUser(ctx, id, me)
		if err != nil {
			continue
		}
		// Hide users who blocked the searcher (anti-harassment).
		if u.BlockedMe {
			continue
		}
		results = append(results, u)
	}
	api.WriteJSON(w, r, http.StatusOK, results)
}

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	u, err := h.publicUser(r.Context(), id, me)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.WriteError(w, r, api.ErrNotFound)
			return
		}
		api.WriteError(w, r, err)
		return
	}
	api.WriteJSON(w, r, http.StatusOK, u)
}

// ---------------------------------------------------------------------------
// Shared user projection
// ---------------------------------------------------------------------------

// publicUser loads a privacy-filtered profile. viewer is the requesting user
// (used for contact status and presence visibility).
func (h *Handler) publicUser(ctx context.Context, id, viewer uuid.UUID) (PublicUser, error) {
	var u PublicUser
	var avatarID *uuid.UUID
	var lastSeen *time.Time
	var createdAt time.Time
	var onlineVis string
	var lastSeenVis string

	err := h.pool.QueryRow(ctx, `
		SELECT u.id, u.username, p.display_name, p.about, p.avatar_attachment_id, u.last_seen_at, u.created_at,
		       s.online_visibility, s.last_seen_visibility
		FROM users u
		JOIN user_profiles p ON p.user_id = u.id
		JOIN user_privacy_settings s ON s.user_id = u.id
		WHERE u.id = $1 AND u.account_status = 'active'
	`, id).Scan(&u.ID, &u.Username, &u.DisplayName, &u.About, &avatarID, &lastSeen, &createdAt,
		&onlineVis, &lastSeenVis)
	if err != nil {
		return u, err
	}
	if avatarID != nil {
		url := "/v1/attachments/" + avatarID.String() + "/download"
		u.AvatarURL = &url
	}
	u.CreatedAt = createdAt.UTC().Format(time.RFC3339)

	// Determine relationship for visibility decisions.
	var isContact, blockedByMe, blockedMe bool
	_ = h.pool.QueryRow(ctx, `
		SELECT
			EXISTS(SELECT 1 FROM contacts WHERE owner_id = $1 AND contact_id = $2),
			EXISTS(SELECT 1 FROM blocked_users WHERE blocker_id = $1 AND blocked_id = $2),
			EXISTS(SELECT 1 FROM blocked_users WHERE blocker_id = $2 AND blocked_id = $1)
	`, viewer, id).Scan(&isContact, &blockedByMe, &blockedMe)
	u.IsContact = isContact
	u.BlockedByMe = blockedByMe
	u.BlockedMe = blockedMe

	// Presence respects privacy: everyone | contacts | nobody.
	if onlineVis == "everyone" || (onlineVis == "contacts" && (isContact || viewer == id)) {
		u.Online = h.presence.IsOnline(ctx, id)
	}
	if lastSeenVis == "everyone" || (lastSeenVis == "contacts" && (isContact || viewer == id)) {
		if lastSeen != nil {
			ts := lastSeen.UTC().Format(time.RFC3339)
			u.LastSeenAt = &ts
		}
	}
	return u, nil
}

// PublicUserWithStatus is exported for other packages needing the projection.
func (h *Handler) PublicUserWithStatus(ctx context.Context, id, viewer uuid.UUID) (PublicUser, error) {
	return h.publicUser(ctx, id, viewer)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func collectIDs(rows pgx.Rows) []uuid.UUID {
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func isEmailQuery(q string) bool {
	return strings.Count(q, "@") == 1 && strings.Contains(q, ".") && !strings.HasPrefix(q, "@")
}

// ---------------------------------------------------------------------------
// Avatar upload
// ---------------------------------------------------------------------------

// AvatarSetter lets the users handler store an uploaded avatar. Implemented
// by the attachments handler to reuse validation/storage.
type AvatarSetter interface {
	StoreAvatar(ctx context.Context, userID uuid.UUID, file multipartFile, maxBytes int64) (attachmentID uuid.UUID, err error)
}

// multipartFile mirrors *multipart.FileHeader without importing multipart here.
type multipartFile interface{}

// SetAvatar wires an uploaded attachment id into the user profile.
func (h *Handler) SetAvatar(ctx context.Context, userID, attachmentID uuid.UUID) error {
	_, err := h.pool.Exec(ctx, `
		UPDATE user_profiles SET avatar_attachment_id = $1, updated_at = now() WHERE user_id = $2
	`, attachmentID, userID)
	return err
}
