// Command api runs the Veylo backend: HTTP API + WebSocket server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/singhtanishq/veylo/services/api/internal/app"
	"github.com/singhtanishq/veylo/services/api/internal/platform/config"
	"github.com/singhtanishq/veylo/services/api/internal/platform/database"
	"github.com/singhtanishq/veylo/services/api/internal/platform/mailer"
	"github.com/singhtanishq/veylo/services/api/internal/platform/redisx"
	"github.com/singhtanishq/veylo/services/api/internal/platform/storage"
)

func main() {
	cfg := config.Load()
	cfg.ValidateWarn()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	applied, err := database.Migrate(ctx, pool, cfg.MigrationsPath)
	if err != nil {
		log.Error("migrations failed", "err", err)
		os.Exit(1)
	}
	if len(applied) > 0 {
		log.Info("migrations applied", "count", len(applied))
	}

	rdb, err := redisx.New(ctx, cfg.RedisURL)
	if err != nil {
		log.Error("redis connect failed", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	store, err := storage.NewLocal(cfg.StoragePath)
	if err != nil {
		log.Error("storage init failed", "err", err)
		os.Exit(1)
	}

	mail := &mailer.Mailer{Host: cfg.SMTPHost, Port: cfg.SMTPPort, From: cfg.SMTPFrom, Mailpit: cfg.MailpitWebURL}

	built := app.Build(app.Deps{
		Pool:   pool,
		Redis:  rdb,
		Store:  store,
		Cfg:    cfg,
		Log:    log,
		Mailer: mail,
	})

	srv := &http.Server{
		Addr:              cfg.Host + ":" + itoa(cfg.Port),
		Handler:           built.Router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("veylo api listening", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	built.Hub.Shutdown()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("shutdown error", "err", err)
	}
	log.Info("bye")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
