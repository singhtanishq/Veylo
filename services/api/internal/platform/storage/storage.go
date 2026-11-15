// Package storage implements the local filesystem storage backend behind a
// narrow interface that can later be backed by S3-compatible storage without
// touching message or attachment logic.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrNotFound is returned when the object does not exist.
var ErrNotFound = errors.New("storage: object not found")

// Storage abstracts binary object storage.
type Storage interface {
	Save(ctx context.Context, key string, r io.Reader, size int64) error
	Open(ctx context.Context, key string) (io.ReadSeekCloser, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (int64, error)
}

// Local stores objects beneath a configurable root directory.
type Local struct {
	root string
}

// NewLocal creates the storage root (including subdirectories) if needed.
func NewLocal(root string) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("storage: mkdir: %w", err)
	}
	return &Local{root: abs}, nil
}

// resolve maps a logical key to a path inside root, rejecting traversal.
func (l *Local) resolve(key string) (string, error) {
	clean := filepath.Clean("/" + strings.TrimPrefix(key, "/"))
	full := filepath.Join(l.root, clean)
	if !strings.HasPrefix(full, l.root+string(os.PathSeparator)) && full != l.root {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	return full, nil
}

func (l *Local) Save(_ context.Context, key string, r io.Reader, _ int64) error {
	path, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func (l *Local) Open(_ context.Context, key string) (io.ReadSeekCloser, error) {
	path, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

func (l *Local) Delete(_ context.Context, key string) error {
	path, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (l *Local) Stat(_ context.Context, key string) (int64, error) {
	path, err := l.resolve(key)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return info.Size(), nil
}

// ---------------------------------------------------------------------------
// Upload validation helpers
// ---------------------------------------------------------------------------

var unsafeNameChars = regexp.MustCompile(`[^\p{L}\p{N}\._\- ]+`)

// SanitizeName strips path components and dangerous characters from a
// client-supplied filename.
func SanitizeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = unsafeNameChars.ReplaceAllString(name, "_")
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ". ")
	if name == "" {
		name = "file"
	}
	const maxName = 120
	if len(name) > maxName {
		name = name[len(name)-maxName:]
	}
	return name
}

// DetectType sniffs the real content type from the first bytes of the file.
// Frontend-supplied MIME types are never trusted.
func DetectType(file multipart.File) (string, error) {
	defer func() { _, _ = file.Seek(0, io.SeekStart) }()
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}
	_, _ = file.Seek(0, io.SeekStart)
	ct := http.DetectContentType(buf[:n])
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	return ct, nil
}

// AllowedKinds maps broad kinds to permitted concrete MIME types.
var AllowedKinds = map[string][]string{
	"image": {"image/jpeg", "image/png", "image/gif", "image/webp"},
	"video": {"video/mp4", "video/webm", "video/quicktime"},
	"audio": {"audio/mpeg", "audio/ogg", "audio/wav", "audio/x-wav", "audio/mp4", "audio/webm"},
	"voice": {"audio/mpeg", "audio/ogg", "audio/wav", "audio/x-wav", "audio/mp4", "audio/webm"},
	"document": {"application/pdf", "text/plain", "text/csv", "text/markdown", "application/json",
		"application/msword", "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-excel", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/zip", "application/x-7z-compressed"},
}

// KindForType returns the attachment kind bucket for a MIME type, or "".
func KindForType(mime string) string {
	for kind, types := range AllowedKinds {
		for _, t := range types {
			if t == mime {
				return kind
			}
		}
	}
	return ""
}

// KindForAttachment maps an attachment MIME type + explicit client kind to the
// message type used for rendering (voice messages keep their own bucket).
func KindForAttachment(mime, clientKind string) string {
	if clientKind == "voice" {
		if k := KindForType(mime); k == "audio" || k == "voice" {
			return "voice"
		}
		return ""
	}
	return KindForType(mime)
}
