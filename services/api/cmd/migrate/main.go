// Command migrate applies Veylo database migrations and prints their status.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singhtanishq/veylo/services/api/internal/platform/database"
)

func main() {
	dir := flag.String("dir", "./migrations", "migrations directory")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://veylo:veylo_local_dev@localhost:5432/veylo?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	applied, err := database.Migrate(ctx, pool, *dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	if len(applied) == 0 {
		fmt.Println("migrations: already up to date")
		return
	}
	for _, name := range applied {
		fmt.Println("applied:", name)
	}
	fmt.Printf("migrations: %d new\n", len(applied))
}
