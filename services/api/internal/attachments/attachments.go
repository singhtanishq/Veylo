// Package attachments implements secure media handling on local filesystem
// storage: server-side content sniffing, size limits, sanitized storage keys,
// membership-based download authorization and image thumbnails.
package attachments

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"

	"github.com/singhtanishq/veylo/services/api/internal/api"
	"github.com/singhtanishq/veylo/services/api/internal/conversations"
	"github.com/singhtanishq/veylo/services/api/internal/platform/config"
	"github.com/singhtanishq/veylo/services/api/internal/platform/ratelimit"
	"github.com/singhtanishq/veylo/services/api/internal/platform/storage"
	"image/png"
)

// Handler exposes attachment endpoints.
type Handler struct {
	pool   *pgxpool.Pool
	store  storage.Storage
	cfg    *config.Config
	conv   *conversations.Handler
	limits *ratelimit.Limiter
}

// NewHandler constructs the attachments handler.
func NewHandler(pool *pgxpool.Pool, store storage.Storage, cfg *config.Config, conv *conversations.Handler, l *ratelimit.Limiter) *Handler {
	return &Handler{pool: pool, store: store, cfg: cfg, conv: conv, limits: l}
}

// Routes registers attachment endpoints (auth applied by router).
func (h *Handler) Routes(r chi.Router) {
	r.Post("/attachments", h.upload)
	r.Get("/attachments/{attachmentID}/download", h.download)
	r.Get("/attachments/{attachmentID}/thumbnail", h.thumbnail)
	r.Delete("/attachments/{attachmentID}", h.remove)
}

// Attachment is the API shape of uploaded media metadata.
type Attachment struct {
	ID           string  `json:"id"`
	FileName     string  `json:"file_name"`
	MimeType     string  `json:"mime_type"`
	Kind         string  `json:"kind"`
	SizeBytes    int64   `json:"size_bytes"`
	Width        *int    `json:"width,omitempty"`
	Height       *int    `json:"height,omitempty"`
	DownloadURL  string  `json:"download_url"`
	ThumbnailURL *string `json:"thumbnail_url,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	me := api.UserID(r.Context())
	if !h.limits.Allow("attach:"+me.String(), ratelimit.PolicyAttach.PerSec, ratelimit.PolicyAttach.Burst) {
		api.WriteError(w, r, api.ErrRateLimited)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxAttachmentBytes()+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			api.WriteError(w, r, api.ErrPayloadTooBig)
			return
		}
		api.WriteError(w, r, api.Errf(http.StatusBadRequest, api.CodeBadRequest,
			"Expected multipart/form-data with a `file` field.", nil))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		api.WriteError(w, r, api.Errf(http.StatusBadRequest, api.CodeBadRequest,
			"Missing `file` field.", nil))
		return
	}
	defer file.Close()

	// Size limit enforced again against the actual read.
	limited := io.LimitReader(file, h.cfg.MaxAttachmentBytes()+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		api.WriteError(w, r, api.ErrPayloadTooBig)
		return
	}
	if int64(len(data)) > h.cfg.MaxAttachmentBytes() {
		api.WriteError(w, r, api.ErrPayloadTooBig)
		return
	}

	// Sniff the real content type; never trust the client MIME.
	ct, err := sniffType(data)
	if err != nil {
		api.WriteError(w, r, api.ErrUnsupported)
		return
	}
	kind := storage.KindForType(ct)
	if kind == "" {
		api.WriteError(w, r, api.Errf(http.StatusUnsupportedMediaType, api.CodeUnsupportedMedia,
			"This file type is not allowed. Allowed: images, video, audio, PDF, text, office documents, zip.",
			map[string]any{"detected": ct}))
		return
	}

	// Dimensions for images.
	var width, height *int
	if kind == "image" && ct != "image/webp" {
		cfgImg, _, err := image.DecodeConfig(strings.NewReader(string(data)))
		if err == nil && cfgImg.Width > 0 && cfgImg.Height > 0 {
			w, hgt := cfgImg.Width, cfgImg.Height
			width, height = &w, &hgt
		}
	}

	name := storage.SanitizeName(header.Filename)
	sum := sha256.Sum256(data)
	now := time.Now().UTC()
	key := fmt.Sprintf("uploads/%04d/%02d/%s/%s", now.Year(), now.Month(), uuid.NewString(), name)

	if err := h.store.Save(r.Context(), key, strings.NewReader(string(data)), int64(len(data))); err != nil {
		api.WriteError(w, r, err)
		return
	}

	var attID uuid.UUID
	err = h.pool.QueryRow(r.Context(), `
		INSERT INTO attachments (owner_id, storage_key, file_name, mime_type, kind, size_bytes, sha256, width, height)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id
	`, me, key, name, ct, kind, len(data), hex.EncodeToString(sum[:]), width, height).Scan(&attID)
	if err != nil {
		_ = h.store.Delete(r.Context(), key)
		api.WriteError(w, r, err)
		return
	}

	att := Attachment{
		ID: attID.String(), FileName: name, MimeType: ct, Kind: kind, SizeBytes: int64(len(data)),
		Width: width, Height: height,
		DownloadURL: "/v1/attachments/" + attID.String() + "/download",
		CreatedAt:   now.Format(time.RFC3339),
	}
	if kind == "image" {
		t := "/v1/attachments/" + attID.String() + "/thumbnail"
		att.ThumbnailURL = &t
	}
	api.WriteJSON(w, r, http.StatusCreated, att)
}

func sniffType(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty upload")
	}
	ct := http.DetectContentType(data)
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	// Office documents / zip-based formats are detected as application/zip.
	return ct, nil
}

// authorize checks that the requester may access the attachment: as owner, as
// an avatar reference on a visible profile, or as a member of any conversation
// containing a message that carries it.
func (h *Handler) authorize(r *http.Request, attID uuid.UUID) (Attachment, error) {
	me := api.UserID(r.Context())
	var att Attachment
	var key string
	var createdAt time.Time
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, storage_key, file_name, mime_type, kind, size_bytes, width, height, created_at
		FROM attachments WHERE id = $1
	`, attID).Scan(&attID, &key, &att.FileName, &att.MimeType, &att.Kind, &att.SizeBytes, &att.Width, &att.Height, &createdAt)
	if err != nil {
		return att, api.ErrNotFound
	}
	att.ID = attID.String()
	att.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	att.DownloadURL = "/v1/attachments/" + attID.String() + "/download"

	var allowed bool
	// Owner.
	if err := h.pool.QueryRow(r.Context(),
		`SELECT owner_id = $2 FROM attachments WHERE id = $1`, attID, me).Scan(&allowed); err == nil && allowed {
		return att, nil
	}
	// Avatar on any visible profile or conversation.
	if err := h.pool.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM user_profiles WHERE avatar_attachment_id = $1) OR
		       EXISTS(SELECT 1 FROM conversations WHERE avatar_attachment_id = $1)
	`, attID).Scan(&allowed); err == nil && allowed {
		return att, nil
	}
	// Attached to a message in one of my conversations.
	if err := h.pool.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM message_attachments ma
			JOIN messages m ON m.id = ma.message_id
			JOIN conversation_members cm ON cm.conversation_id = m.conversation_id
			WHERE ma.attachment_id = $1 AND cm.user_id = $2 AND cm.left_at IS NULL
		)
	`, attID, me).Scan(&allowed); err == nil && allowed {
		return att, nil
	}
	return att, api.ErrForbidden
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	attID, err := uuid.Parse(chi.URLParam(r, "attachmentID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	att, err := h.authorize(r, attID)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	var key string
	if err := h.pool.QueryRow(r.Context(),
		`SELECT storage_key FROM attachments WHERE id = $1`, attID).Scan(&key); err != nil {
		api.WriteError(w, r, api.ErrNotFound)
		return
	}
	f, err := h.store.Open(r.Context(), key)
	if err != nil {
		api.WriteError(w, r, api.ErrNotFound)
		return
	}
	defer f.Close()

	disposition := "attachment"
	switch {
	case att.Kind == "image", att.Kind == "video", att.Kind == "audio", att.Kind == "voice",
		att.MimeType == "application/pdf", att.MimeType == "text/plain":
		disposition = "inline"
	}
	w.Header().Set("Content-Type", att.MimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", att.SizeBytes))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, sanitizeCD(att.FileName)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Del("X-Frame-Options") // allow inline previews in the app shell
	http.ServeContent(w, r, att.FileName, time.Now(), f)
}

func (h *Handler) thumbnail(w http.ResponseWriter, r *http.Request) {
	attID, err := uuid.Parse(chi.URLParam(r, "attachmentID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	att, err := h.authorize(r, attID)
	if err != nil || att.Kind != "image" {
		api.WriteError(w, r, api.ErrNotFound)
		return
	}
	var key string
	if err := h.pool.QueryRow(r.Context(),
		`SELECT storage_key FROM attachments WHERE id = $1`, attID).Scan(&key); err != nil {
		api.WriteError(w, r, api.ErrNotFound)
		return
	}
	f, err := h.store.Open(r.Context(), key)
	if err != nil {
		api.WriteError(w, r, api.ErrNotFound)
		return
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		// WebP and friends: fall back to the original.
		h.download(w, r)
		return
	}
	thumb := resizeMax(src, 512)
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_ = encodePNG(w, thumb)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	attID, err := uuid.Parse(chi.URLParam(r, "attachmentID"))
	if err != nil {
		api.WriteError(w, r, api.ErrBadRequest)
		return
	}
	me := api.UserID(r.Context())
	ctx := r.Context()

	var key string
	var owner uuid.UUID
	err = h.pool.QueryRow(ctx,
		`SELECT storage_key, owner_id FROM attachments WHERE id = $1`, attID).Scan(&key, &owner)
	if err != nil {
		api.WriteError(w, r, api.ErrNotFound)
		return
	}
	if owner != me {
		api.WriteError(w, r, api.ErrForbidden)
		return
	}
	var used bool
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM message_attachments WHERE attachment_id = $1)`, attID).Scan(&used); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if used {
		api.WriteError(w, r, api.Errf(http.StatusConflict, api.CodeConflict,
			"This attachment belongs to a message and cannot be removed.", nil))
		return
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM attachments WHERE id = $1`, attID); err != nil {
		api.WriteError(w, r, err)
		return
	}
	_ = h.store.Delete(ctx, key)
	api.WriteJSON(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// ---------------------------------------------------------------------------
// image helpers
// ---------------------------------------------------------------------------

func resizeMax(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	w, hgt := b.Dx(), b.Dy()
	scale := 1.0
	if w > maxEdge || hgt > maxEdge {
		scale = float64(maxEdge) / float64(max(w, hgt))
	}
	nw, nh := max(1, int(float64(w)*scale)), max(1, int(float64(hgt)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

func encodePNG(w io.Writer, img image.Image) error {
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	return enc.Encode(w, img)
}

func sanitizeCD(name string) string {
	name = strings.ReplaceAll(name, `"`, "'")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	if name == "" {
		return "file"
	}
	return name
}
