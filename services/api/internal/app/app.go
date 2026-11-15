// Package app assembles the complete Veylo backend (router, hub, handlers)
// so both the main command and integration tests build the same server.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/singhtanishq/veylo/services/api/internal/api"
	"github.com/singhtanishq/veylo/services/api/internal/attachments"
	"github.com/singhtanishq/veylo/services/api/internal/audit"
	"github.com/singhtanishq/veylo/services/api/internal/auth"
	"github.com/singhtanishq/veylo/services/api/internal/contacts"
	"github.com/singhtanishq/veylo/services/api/internal/conversations"
	"github.com/singhtanishq/veylo/services/api/internal/messages"
	"github.com/singhtanishq/veylo/services/api/internal/notifications"
	"github.com/singhtanishq/veylo/services/api/internal/platform/config"
	"github.com/singhtanishq/veylo/services/api/internal/platform/mailer"
	"github.com/singhtanishq/veylo/services/api/internal/platform/ratelimit"
	"github.com/singhtanishq/veylo/services/api/internal/platform/storage"
	"github.com/singhtanishq/veylo/services/api/internal/presence"
	"github.com/singhtanishq/veylo/services/api/internal/realtime"
	"github.com/singhtanishq/veylo/services/api/internal/receipts"
	"github.com/singhtanishq/veylo/services/api/internal/search"
	"github.com/singhtanishq/veylo/services/api/internal/settings"
	"github.com/singhtanishq/veylo/services/api/internal/users"
)

// App bundles the assembled server pieces.
type App struct {
	Router *chi.Mux
	Hub    *realtime.Hub
	Cfg    *config.Config
}

// Deps are the infrastructure pieces Build needs.
type Deps struct {
	Pool   *pgxpool.Pool
	Redis  *redis.Client
	Store  storage.Storage
	Cfg    *config.Config
	Log    *slog.Logger
	Mailer MailSender
}

// MailSender is the subset of the mailer used by auth.
type MailSender interface {
	Send(msg mailer.Message) error
}

// Build wires every domain handler and returns the app.
func Build(d Deps) *App {
	cfg := d.Cfg
	log := d.Log
	if log == nil {
		log = slog.Default()
	}

	auditor := audit.New(d.Pool, log)
	var limiter *ratelimit.Limiter
	if cfg.Env == "test" {
		limiter = ratelimit.NewDisabled()
	} else {
		limiter = ratelimit.New(10 * time.Minute)
	}
	presenceStore := presence.NewStore(d.Redis)

	authH := auth.NewHandler(d.Pool, cfg, d.Mailer, auditor, limiter)
	usersH := users.NewHandler(d.Pool, limiter, presenceStore)
	notifier := notifications.New(d.Pool, nil)
	contactsH := contacts.NewHandler(d.Pool, presenceStore, notifier)
	convH := conversations.NewHandler(d.Pool, nil, presenceStore, notifier)
	msgsH := messages.NewHandler(d.Pool, convH, notifier, nil, limiter)
	receiptsH := receipts.NewHandler(d.Pool, nil, readPrivacyChecker(d.Pool))
	settingsH := settings.NewHandler(d.Pool)
	searchH := search.NewHandler(d.Pool)
	notifH := notifications.NewHandler(d.Pool)
	attH := attachments.NewHandler(d.Pool, d.Store, cfg, convH, limiter)

	hub := realtime.NewHub(d.Pool, d.Redis, presenceStore, log, msgsH, receiptsH, readPrivacyChecker(d.Pool))
	convH.SetHub(hub)
	msgsH.SetHub(hub)
	receiptsH.SetHub(hub)
	notifier.SetFanout(hub)
	wsH := realtime.NewHandler(hub)

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(requestIDBridge)
	r.Use(chimw.Recoverer)
	r.Use(api.RequestLogger(log))
	r.Use(api.SecurityHeaders)
	r.Use(api.CORS(cfg.AllowedOrigins))
	r.Use(api.OriginCheck(cfg.AllowedOrigins))

	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		api.WriteJSON(w, req, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		if err := hub.Healthy(req.Context()); err != nil {
			api.WriteError(w, req, api.Errf(http.StatusServiceUnavailable, "unavailable", err.Error(), nil))
			return
		}
		api.WriteJSON(w, req, http.StatusOK, map[string]string{"status": "ready"})
	})

	r.Route("/v1", func(r chi.Router) {
		authH.Routes(r)
		r.Group(func(r chi.Router) {
			r.Use(authH.RequireAuth)
			usersH.Routes(r)
			contactsH.Routes(r)
			convH.Routes(r)
			msgsH.Routes(r)
			receiptsH.Routes(r)
			attH.Routes(r)
			searchH.Routes(r)
			settingsH.Routes(r)
			notifH.Routes(r)
			wsH.TokenRoutes(r)
		})
		wsH.WSRoutes(r)
	})

	return &App{Router: r, Hub: hub, Cfg: cfg}
}

// readPrivacyChecker returns whether the user disabled read receipts.
func readPrivacyChecker(pool *pgxpool.Pool) realtime.ReadPrivacyChecker {
	return func(ctx context.Context, userID uuid.UUID) bool {
		var enabled bool
		err := pool.QueryRow(ctx,
			`SELECT read_receipts_enabled FROM user_privacy_settings WHERE user_id = $1`, userID).Scan(&enabled)
		if err != nil {
			return false
		}
		return !enabled
	}
}

func requestIDBridge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chimw.GetReqID(r.Context())
		next.ServeHTTP(w, r.WithContext(api.SetRequestID(r.Context(), id)))
	})
}
