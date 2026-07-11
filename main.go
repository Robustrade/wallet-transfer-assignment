package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "file:wallets.db?_foreign_keys=1")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	srv := StartHTTPServer(db, ":8080")

	log.Printf("listening on :8080")

	// test helper: optionally shutdown after duration when TEST_RUN_DURATION_MS is set
	if v := os.Getenv("TEST_RUN_DURATION_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			go func() {
				time.Sleep(time.Millisecond * time.Duration(ms))
				_ = srv.Shutdown(context.Background())
			}()
		}
	}

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}

	_ = db.Close()
}

// StartHTTPServer creates and returns an http.Server wired with repositories and handlers.
func StartHTTPServer(db *sql.DB, addr string) *http.Server {
	repo := NewRepository(db)
	svc := NewService(repo)
	h := NewHandler(svc)

	srv := &http.Server{
		Addr:         addr,
		Handler:      h.Router(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	return srv
}
