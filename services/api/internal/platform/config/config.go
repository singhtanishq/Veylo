// Package config loads Veylo API configuration from environment variables
// with local-development defaults.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration for the API service.
type Config struct {
	Env            string // development | production
	Host           string
	Port           int
	DatabaseURL    string
	RedisURL       string
	EncryptionKey  string
	SessionSecret  string
	SMTPHost       string
	SMTPPort       int
	SMTPFrom       string
	MailpitWebURL  string
	AppBaseURL     string
	APIBaseURL     string
	StoragePath    string
	MaxAttachMB    int
	MigrationsPath string

	// Session lifetimes (local development defaults).
	SessionTTLHours    int
	SessionRenewMin    int
	VerificationTTLMin int
	ResetTTLMin        int

	// Allowed browser origins for CORS / origin checks.
	AllowedOrigins []string
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// Load reads configuration from the environment.
func Load() *Config {
	cfg := &Config{
		Env:                getenv("APP_ENV", "development"),
		Host:               getenv("API_HOST", "0.0.0.0"),
		Port:               getenvInt("API_PORT", 8080),
		DatabaseURL:        getenv("DATABASE_URL", "postgres://veylo:veylo_local_dev@localhost:5432/veylo?sslmode=disable"),
		RedisURL:           getenv("REDIS_URL", "redis://localhost:6379/0"),
		EncryptionKey:      os.Getenv("APP_ENCRYPTION_KEY"),
		SessionSecret:      os.Getenv("SESSION_SECRET"),
		SMTPHost:           getenv("SMTP_HOST", "localhost"),
		SMTPPort:           getenvInt("SMTP_PORT", 1025),
		SMTPFrom:           getenv("SMTP_FROM", "Veylo <no-reply@veylo.local>"),
		MailpitWebURL:      getenv("MAILPIT_WEB_URL", "http://localhost:8025"),
		AppBaseURL:         getenv("APP_BASE_URL", "http://localhost:3000"),
		APIBaseURL:         getenv("API_BASE_URL", "http://localhost:8080"),
		StoragePath:        getenv("STORAGE_PATH", "./storage/uploads"),
		MaxAttachMB:        getenvInt("MAX_ATTACHMENT_SIZE_MB", 25),
		MigrationsPath:     getenv("MIGRATIONS_PATH", "./migrations"),
		SessionTTLHours:    getenvInt("SESSION_TTL_HOURS", 24*30),
		SessionRenewMin:    getenvInt("SESSION_RENEW_MINUTES", 15),
		VerificationTTLMin: getenvInt("VERIFICATION_TTL_MINUTES", 60*24),
		ResetTTLMin:        getenvInt("RESET_TTL_MINUTES", 60),
		AllowedOrigins: []string{
			getenv("APP_BASE_URL", "http://localhost:3000"),
			"http://localhost:3000",
			"http://127.0.0.1:3000",
			"http://localhost:3001",
			"http://127.0.0.1:3001",
		},
	}
	return cfg
}

// IsDevelopment reports whether the API runs in development mode.
func (c *Config) IsDevelopment() bool { return c.Env == "development" }

// MaxAttachmentBytes returns the upload size limit in bytes.
func (c *Config) MaxAttachmentBytes() int64 { return int64(c.MaxAttachMB) * 1024 * 1024 }

// ValidateWarn prints a warning for missing production secrets.
func (c *Config) ValidateWarn() {
	if c.SessionSecret == "" || c.EncryptionKey == "" {
		fmt.Println("[veylo] WARN: APP_ENCRYPTION_KEY / SESSION_SECRET not set; generate with `openssl rand -hex 32`")
	}
}
