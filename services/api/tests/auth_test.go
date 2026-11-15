package tests

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestAuthFullFlow covers register → verify → login → me → logout.
func TestAuthFullFlow(t *testing.T) {
	resetDB(t)

	c := newClient()

	// Registration validation errors.
	c.mustJSON(t, "POST", "/v1/auth/register", map[string]any{
		"email": "not-an-email", "password": "weak", "username": "ab", "display_name": "",
	}, http.StatusUnprocessableEntity)

	// Happy path.
	data := asMap(t, c.mustJSON(t, "POST", "/v1/auth/register", map[string]any{
		"email": "flow@example.test", "password": "Str0ngPass!", "username": "flowuser", "display_name": "Flow User",
	}, http.StatusCreated))
	if data["next"] != "verify_email" {
		t.Fatalf("expected next=verify_email, got %v", data["next"])
	}

	// Login blocked before verification.
	c.mustJSON(t, "POST", "/v1/auth/login", map[string]any{
		"identifier": "flow@example.test", "password": "Str0ngPass!",
	}, http.StatusForbidden)

	// Verify via token from the captured email.
	token := lastVerificationToken(t)
	c.mustJSON(t, "POST", "/v1/auth/verify-email", map[string]any{"token": token}, http.StatusOK)

	// Token is single-use.
	c.mustJSON(t, "POST", "/v1/auth/verify-email", map[string]any{"token": token}, http.StatusBadRequest)

	// Login now succeeds and sets the session cookie.
	c.mustJSON(t, "POST", "/v1/auth/login", map[string]any{
		"identifier": "flowuser", "password": "Str0ngPass!",
	}, http.StatusOK)

	// me() returns the verified account.
	me := asMap(t, c.mustJSON(t, "GET", "/v1/auth/me", nil, http.StatusOK))
	if me["email"] != "flow@example.test" || me["email_verified"] != true {
		t.Fatalf("me() wrong: %v", me)
	}

	// Wrong password rejected.
	bad := newClient()
	bad.mustJSON(t, "POST", "/v1/auth/login", map[string]any{
		"identifier": "flow@example.test", "password": "WrongPass1",
	}, http.StatusUnauthorized)

	// Unknown identifier rejected with the same generic error (anti-enumeration).
	bad.mustJSON(t, "POST", "/v1/auth/login", map[string]any{
		"identifier": "ghost@example.test", "password": "WrongPass1",
	}, http.StatusUnauthorized)

	// Logout revokes the session.
	c.mustJSON(t, "POST", "/v1/auth/logout", nil, http.StatusOK)
	c.mustJSON(t, "GET", "/v1/auth/me", nil, http.StatusUnauthorized)
}

func TestAuthDuplicatePrevention(t *testing.T) {
	resetDB(t)
	registerNewUser(t, "dupone")

	c := newClient()
	// Duplicate email.
	c.mustJSON(t, "POST", "/v1/auth/register", map[string]any{
		"email": "dupone@example.test", "password": "Str0ngPass!", "username": "othername", "display_name": "Other",
	}, http.StatusConflict)
	// Duplicate username.
	c.mustJSON(t, "POST", "/v1/auth/register", map[string]any{
		"email": "fresh@example.test", "password": "Str0ngPass!", "username": "dupone", "display_name": "Other",
	}, http.StatusConflict)
	// Username normalized: case-insensitive duplicate.
	c.mustJSON(t, "POST", "/v1/auth/register", map[string]any{
		"email": "fresh2@example.test", "password": "Str0ngPass!", "username": "DuPoNe", "display_name": "Other",
	}, http.StatusConflict)
	// Email normalized: case-insensitive duplicate.
	c.mustJSON(t, "POST", "/v1/auth/register", map[string]any{
		"email": "DuPoNe@EXAMPLE.test", "password": "Str0ngPass!", "username": "thirduser", "display_name": "Other",
	}, http.StatusConflict)
}

func TestPasswordReset(t *testing.T) {
	resetDB(t)
	u := registerNewUser(t, "resetter")

	// Forgot-password responds identically for unknown emails (anti-enumeration).
	ghost := newClient()
	ghost.mustJSON(t, "POST", "/v1/auth/forgot-password", map[string]any{"email": "ghost@example.test"}, http.StatusOK)

	// Real request → token via captured email.
	u.C.mustJSON(t, "POST", "/v1/auth/forgot-password", map[string]any{"email": u.Email}, http.StatusOK)
	token := lastVerificationToken(t)

	// New session from reset is revoked afterwards: log in first to have a session.
	u.C.mustJSON(t, "POST", "/v1/auth/login", map[string]any{"identifier": u.Email, "password": u.Password}, http.StatusOK)

	// Reset with weak password rejected.
	u.C.mustJSON(t, "POST", "/v1/auth/reset-password", map[string]any{"token": token, "password": "weak"}, http.StatusUnprocessableEntity)

	// Reset succeeds.
	newPass := "N3wPass!word"
	u.C.mustJSON(t, "POST", "/v1/auth/reset-password", map[string]any{"token": token, "password": newPass}, http.StatusOK)

	// Token single-use.
	u.C.mustJSON(t, "POST", "/v1/auth/reset-password", map[string]any{"token": token, "password": newPass}, http.StatusBadRequest)

	// Old password fails, new works.
	fresh := newClient()
	fresh.mustJSON(t, "POST", "/v1/auth/login", map[string]any{"identifier": u.Email, "password": u.Password}, http.StatusUnauthorized)
	fresh.mustJSON(t, "POST", "/v1/auth/login", map[string]any{"identifier": u.Email, "password": newPass}, http.StatusOK)
	_ = fmt.Sprint()
	_ = strings.TrimSpace("")
}
