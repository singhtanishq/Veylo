// Package database provides the PostgreSQL connection pool and an embedded
// file-system based migration runner.
package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect creates a pgx connection pool and verifies connectivity.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("database: parse dsn: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}
	return pool, nil
}

// migration is one ordered SQL file.
type migration struct {
	version  int
	filename string
	sql      string
	checksum string
}

// Migrate applies all pending migrations from dir, in filename order.
// Each migration runs inside a transaction and is recorded in
// schema_migrations with a checksum to detect drift.
func Migrate(ctx context.Context, pool *pgxpool.Pool, dir string) (applied []string, err error) {
	migs, err := loadMigrations(dir)
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			filename   TEXT NOT NULL,
			checksum   TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return nil, fmt.Errorf("database: schema_migrations: %w", err)
	}

	for _, m := range migs {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, m.version).Scan(&exists); err != nil {
			return applied, err
		}
		if exists {
			var checksum string
			if err := pool.QueryRow(ctx,
				`SELECT checksum FROM schema_migrations WHERE version=$1`, m.version).Scan(&checksum); err != nil {
				return applied, err
			}
			if checksum != m.checksum {
				return applied, fmt.Errorf("database: migration %03d checksum drift (applied=%s, file=%s)",
					m.version, checksum, m.checksum)
			}
			continue
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return applied, err
		}
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("database: apply %s: %w", m.filename, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version, filename, checksum) VALUES ($1, $2, $3)`,
			m.version, m.filename, m.checksum); err != nil {
			_ = tx.Rollback(ctx)
			return applied, err
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, err
		}
		applied = append(applied, m.filename)
	}
	return applied, nil
}

func loadMigrations(dir string) ([]migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("database: read migrations dir: %w", err)
	}
	var migs []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		version := 0
		if _, err := fmt.Sscanf(e.Name(), "%d", &version); err != nil {
			return nil, fmt.Errorf("database: migration %s must start with a numeric version", e.Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		migs = append(migs, migration{
			version:  version,
			filename: e.Name(),
			sql:      string(data),
			checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	return migs, nil
}

// MigrationsFS is a convenience helper for tools that receive an fs.FS.
func MigrationsFS(fsys fs.FS, dir string) ([]string, error) {
	migs, err := fs.Glob(fsys, filepath.Join(dir, "*.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(migs)
	return migs, nil
}
