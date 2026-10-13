package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"noteit/internal/notes"
	"noteit/internal/platform"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	db, err := platform.OpenDB(ctx, platform.Required("DATABASE_URL"), notes.Migrations)
	cancel()
	if err != nil {
		slog.Error("database initialization failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	key := platform.Required("INTERNAL_API_KEY")
	if len(key) < 32 {
		slog.Error("INTERNAL_API_KEY must be at least 32 characters")
		os.Exit(1)
	}
	s := &notes.Server{DB: db, BaseURL: platform.BaseURL(), Auth: notes.AuthClient{URL: platform.Env("AUTH_URL", "http://auth:8081"), Key: key, Client: &http.Client{Timeout: 4 * time.Second}}}
	platform.Serve(platform.Env("LISTEN_ADDR", ":8082"), s.Handler())
}
