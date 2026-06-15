// cmd/migrate applies or rolls back database migrations.
// Usage: go run ./cmd/migrate -database <dsn> -action <up|down>
package main

import (
	"errors"
	"flag"
	"log"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	dbURL := flag.String("database", "", "PostgreSQL connection URL (required)")
	action := flag.String("action", "up", "Migration direction: up or down")
	flag.Parse()

	if *dbURL == "" {
		log.Fatal("flag -database is required")
	}

	m, err := migrate.New("file://db/migrations", *dbURL)
	if err != nil {
		log.Fatalf("migrate.New: %v", err)
	}
	defer m.Close()

	switch *action {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate up: %v", err)
		}
		log.Println("migrations applied")
	case "down":
		if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate down: %v", err)
		}
		log.Println("migrations rolled back")
	default:
		log.Fatalf("unknown action %q: use 'up' or 'down'", *action)
	}
}
