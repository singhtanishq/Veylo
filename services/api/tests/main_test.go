// Package tests contains Veylo backend integration tests. They exercise the
// real HTTP + WebSocket stack against a real PostgreSQL and Redis, mirroring
// production behavior (no mocks for infrastructure).
//
// Required environment:
//
//	DATABASE_URL  (default: postgres://veylo:veylo_local_dev@localhost:5432/veylo_test?sslmode=disable)
//	REDIS_URL     (default: redis://localhost:6379/1)
//
// Tests are skipped with a clear message when the dependencies are absent.
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/singhtanishq/veylo/services/api/internal/app"
	"github.com/singhtanishq/veylo/services/api/internal/platform/config"
	"github.com/singhtanishq/veylo/services/api/internal/platform/database"
	"github.com/singhtanishq/veylo/services/api/internal/platform/mailer"
	"github.com/singhtanishq/veylo/services/api/internal/platform/storage"
)

var (
	pool    *pgxpool.Pool
	rdb     *redis.Client
	server  *httptest.Server
	baseURL string
)

const testMigrationsPath = "../migrations"

func TestMain(m *testing.M) {
	dsn := envOr("DATABASE_URL", "postgres://veylo:veylo_local_dev@localhost:5432/veylo_test?sslmode=disable")
	redisURL := envOr("REDIS_URL", "redis://localhost:6379/1")

	ctx := context.Background()
	var err error
	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to test postgres (%v)\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: test postgres unreachable (%v)\n", err)
		os.Exit(0)
	}
	if _, err := database.Migrate(ctx, pool, testMigrationsPath); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: migrations failed: %v\n", err)
		os.Exit(1)
	}

	rdb = redis.NewClient(&redis.Options{Addr: redisAddr(redisURL), DB: 1})
	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: test redis unreachable (%v)\n", err)
		os.Exit(0)
	}

	// Local storage under a temp dir.
	storeDir, _ := os.MkdirTemp("", "veylo-test-storage-*")
	store, _ := storage.NewLocal(storeDir)

	cfg := config.Load()
	cfg.Env = "test"
	cfg.AllowedOrigins = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	cfg.StoragePath = storeDir

	built := app.Build(app.Deps{
		Pool:   pool,
		Redis:  rdb,
		Store:  store,
		Cfg:    cfg,
		Log:    slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Mailer: globalMailer,
	})
	server = httptest.NewServer(built.Router)
	baseURL = server.URL

	code := m.Run()

	server.Close()
	_ = rdb.Close()
	pool.Close()
	_ = os.RemoveAll(storeDir)
	os.Exit(code)
}

// recordingMailer captures emails so tests can extract verification tokens
// from the links (mirrors what Mailpit does in real local runs).
type recordingMailer struct {
	mu       sync.Mutex
	messages []mailer.Message
}

func (m *recordingMailer) Send(msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

var globalMailer = &recordingMailer{}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func redisAddr(url string) string {
	// redis://localhost:6379/1 → localhost:6379
	s := strings.TrimPrefix(url, "redis://")
	if i := strings.Index(s, "/"); i >= 0 {
		return s[:i]
	}
	return s
}

// resetDB truncates all tables between tests (order-independent via CASCADE).
func resetDB(t *testing.T) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		TRUNCATE users, sessions, contacts, contact_requests, blocked_users,
		         conversations, attachments, notifications,
		         user_preferences, user_privacy_settings, audit_logs
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("reset db: %v", err)
	}
	_, _ = rdb.FlushDB(context.Background()).Result()
}

// client is an HTTP test client with a cookie jar (one browser session).
type client struct {
	t      *http.Client
	base   string
	header map[string]string
}

func newClient() *client {
	jar, _ := cookiejar.New(nil)
	return &client{
		t:    &http.Client{Jar: jar, Timeout: 15 * time.Second},
		base: baseURL,
	}
}

// mustJSON performs a request and decodes the {"data":...} envelope.
func (c *client) mustJSON(t *testing.T, method, path string, body any, wantStatus int) any {
	t.Helper()
	resp, data := c.do(t, method, path, body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: status %d (want %d): %s", method, path, resp.StatusCode, wantStatus, string(data))
	}
	var env struct {
		Data  any `json:"data"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("%s %s: bad json: %v — %s", method, path, err, string(data))
	}
	return env.Data
}

// asMap asserts the payload is an object.
func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T: %v", v, v)
	}
	return m
}

// asList asserts the payload is an array of objects.
func asList(t *testing.T, v any) []map[string]any {
	t.Helper()
	items, ok := v.([]any)
	if !ok {
		t.Fatalf("expected array, got %T: %v", v, v)
	}
	out := []map[string]any{}
	for _, it := range items {
		out = append(out, asMap(t, it))
	}
	return out
}

func (c *client) do(t *testing.T, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:3000")
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	resp, err := c.t.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

type registeredUser struct {
	ID       string
	Email    string
	Username string
	Password string
	C        *client
}

var userSeq = 0

// registerNewUser registers + verifies + logs in a fresh user.
func registerNewUser(t *testing.T, username string) *registeredUser {
	t.Helper()
	userSeq++
	if username == "" {
		username = fmt.Sprintf("user%03d", userSeq)
	}
	email := fmt.Sprintf("%s@example.test", username)
	password := "Passw0rd!x"
	display := strings.Title(username) //nolint:staticcheck // test helper

	c := newClient()
	data := asMap(t, c.mustJSON(t, "POST", "/v1/auth/register", map[string]any{
		"email": email, "password": password, "username": username, "display_name": display,
	}, http.StatusCreated))
	uid, _ := data["user_id"].(string)

	// Extract the verification token from the captured email.
	token := lastVerificationToken(t)
	c.mustJSON(t, "POST", "/v1/auth/verify-email", map[string]any{"token": token}, http.StatusOK)

	// Login sets the session cookie in the jar.
	c.mustJSON(t, "POST", "/v1/auth/login", map[string]any{
		"identifier": email, "password": password,
	}, http.StatusOK)

	return &registeredUser{ID: uid, Email: email, Username: username, Password: password, C: c}
}

var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

func lastVerificationToken(t *testing.T) string {
	t.Helper()
	globalMailer.mu.Lock()
	defer globalMailer.mu.Unlock()
	if len(globalMailer.messages) == 0 {
		t.Fatal("no emails captured")
	}
	msg := globalMailer.messages[len(globalMailer.messages)-1]
	globalMailer.messages = globalMailer.messages[:len(globalMailer.messages)-1]
	m := tokenRe.FindStringSubmatch(msg.Text)
	if m == nil {
		t.Fatalf("no token in email: %q", msg.Text)
	}
	return m[1]
}

func (u *registeredUser) conversations(t *testing.T) []map[string]any {
	t.Helper()
	return asList(t, u.C.mustJSON(t, "GET", "/v1/conversations", nil, http.StatusOK))
}

func (u *registeredUser) ws(t *testing.T) *wsClient {
	t.Helper()
	ticketData := asMap(t, u.C.mustJSON(t, "POST", "/v1/ws/token", nil, http.StatusOK))
	ticket, _ := ticketData["ticket"].(string)
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/v1/ws?ticket=" + ticket
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	w := &wsClient{conn: conn}
	// Wait for connection.ready.
	env, err := w.readEnvelope(5 * time.Second)
	if err != nil {
		t.Fatalf("connection.ready: %v", err)
	}
	if env["type"] != "connection.ready" {
		t.Fatalf("expected connection.ready, got %v", env["type"])
	}
	return w
}

// wsClient wraps a WebSocket with helper readers/writers.
type wsClient struct {
	conn *websocket.Conn
}

type envelope map[string]any

func (w *wsClient) send(t *testing.T, typ string, data map[string]any) {
	t.Helper()
	payload := map[string]any{
		"v": 1, "type": typ, "event_id": uuid.NewString(), "data": data,
	}
	w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := w.conn.WriteJSON(payload); err != nil {
		t.Fatalf("ws write %s: %v", typ, err)
	}
}

func (w *wsClient) readEnvelope(timeout time.Duration) (envelope, error) {
	w.conn.SetReadDeadline(time.Now().Add(timeout))
	var env envelope
	if err := w.conn.ReadJSON(&env); err != nil {
		return nil, err
	}
	return env, nil
}

// waitFor polls incoming envelopes until one of wantType arrives; others are
// returned via sink for optional inspection.
func (w *wsClient) waitFor(t *testing.T, timeout time.Duration, wantType string) envelope {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		w.conn.SetReadDeadline(time.Now().Add(time.Until(deadline)))
		var env envelope
		if err := w.conn.ReadJSON(&env); err != nil {
			t.Fatalf("waiting for %s: %v", wantType, err)
		}
		if env["type"] == wantType {
			return env
		}
	}
}

func (w *wsClient) close() { _ = w.conn.Close() }

func testCtx() context.Context { return context.Background() }

// rawList converts an array payload into a list of maps.
func rawList(t *testing.T, data any) []map[string]any {
	return asList(t, data)
}

// envData unwraps the WS envelope "data" object.
func envData(t *testing.T, env envelope) map[string]any {
	t.Helper()
	d, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("ws envelope missing data object: %v", env)
	}
	return d
}
