// Package api provides shared HTTP plumbing: JSON responses, middleware and
// request context helpers for all Veylo domain handlers.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// ---------------------------------------------------------------------------
// Standard error codes
// ---------------------------------------------------------------------------

const (
	CodeValidation       = "validation_error"
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeEmailExists      = "email_exists"
	CodeUsernameExists   = "username_exists"
	CodeEmailNotVerified = "email_not_verified"
	CodeRateLimited      = "rate_limited"
	CodeBadRequest       = "bad_request"
	CodeInternal         = "internal_error"
	CodeBlocked          = "blocked"
	CodeTooLarge         = "payload_too_large"
	CodeUnsupportedMedia = "unsupported_media_type"
)

// APIError is a machine-readable error with an HTTP status.
type APIError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// Errf builds an *APIError.
func Errf(status int, code, msg string, details any) *APIError {
	return &APIError{Status: status, Code: code, Message: msg, Details: details}
}

var (
	ErrUnauthorized  = Errf(http.StatusUnauthorized, CodeUnauthorized, "Authentication required.", nil)
	ErrForbidden     = Errf(http.StatusForbidden, CodeForbidden, "You do not have access to this resource.", nil)
	ErrNotFound      = Errf(http.StatusNotFound, CodeNotFound, "Resource not found.", nil)
	ErrBadRequest    = Errf(http.StatusBadRequest, CodeBadRequest, "Malformed request.", nil)
	ErrInternal      = Errf(http.StatusInternalServerError, CodeInternal, "Something went wrong on our side.", nil)
	ErrValidation    = Errf(http.StatusUnprocessableEntity, CodeValidation, "Please check the highlighted fields.", nil)
	ErrRateLimited   = Errf(http.StatusTooManyRequests, CodeRateLimited, "Too many requests. Please slow down.", nil)
	ErrPayloadTooBig = Errf(http.StatusRequestEntityTooLarge, CodeTooLarge, "The upload exceeds the size limit.", nil)
	ErrUnsupported   = Errf(http.StatusUnsupportedMediaType, CodeUnsupportedMedia, "This file type is not supported.", nil)
	ErrBlockedBy     = Errf(http.StatusForbidden, CodeBlocked, "This action is not available between blocked users.", nil)
)

// AsAPIError maps unexpected errors (e.g. unique violations) into API errors.
func AsAPIError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Errf(http.StatusConflict, CodeConflict, "This resource already exists.", nil)
	}
	return ErrInternal
}

// ---------------------------------------------------------------------------
// Response envelope
// ---------------------------------------------------------------------------

type envelope struct {
	Data      any    `json:"data,omitempty"`
	Error     any    `json:"error,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// WriteJSON writes a success payload.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Data: data, RequestID: RequestID(r.Context())})
}

// WriteError writes an error payload from any error.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := AsAPIError(err)
	if apiErr.Status >= http.StatusInternalServerError {
		// Server-side observability: log unexpected failures with request ID.
		// The client only ever sees the generic message.
		slog.Error("internal error",
			"request_id", RequestID(r.Context()),
			"path", r.URL.Path,
			"err", err.Error(),
		)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(apiErr.Status)
	_ = json.NewEncoder(w).Encode(envelope{
		Error:     apiErr,
		RequestID: RequestID(r.Context()),
	})
}

// WritePage writes a cursor-paginated payload.
func WritePage(w http.ResponseWriter, r *http.Request, data any, nextCursor string, hasMore bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(envelope{
		Data: map[string]any{
			"items":       data,
			"next_cursor": nextCursor,
			"has_more":    hasMore,
		},
		RequestID: RequestID(r.Context()),
	})
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxUserID
	ctxSessionID
)

// RequestID returns the current request ID.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

// SetRequestID stores the request ID.
func SetRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxRequestID, id)
}

// UserID returns the authenticated user ID from context.
func UserID(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(ctxUserID).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

// SetUserID stores the authenticated user ID.
func SetUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxUserID, id)
}

// SessionID returns the authenticated session ID from context.
func SessionID(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(ctxSessionID).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

// SetSessionID stores the authenticated session ID.
func SetSessionID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxSessionID, id)
}

// DecodeJSON decodes a JSON body into dst with a size limit.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return Errf(http.StatusBadRequest, CodeBadRequest, "Malformed JSON body.", nil)
	}
	return nil
}
