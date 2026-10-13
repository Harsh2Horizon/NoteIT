package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"noteit/internal/auth"
	"noteit/internal/platform"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	db, err := platform.OpenDB(ctx, platform.Required("DATABASE_URL"), auth.Migrations)
	cancel()
	if err != nil {
		slog.Error("database initialization failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	base := platform.BaseURL()
	key := platform.Required("INTERNAL_API_KEY")
	if len(key) < 32 {
		slog.Error("INTERNAL_API_KEY must be at least 32 characters")
		os.Exit(1)
	}
	from, err := platform.Email(platform.Env("SMTP_FROM", "hello@noteit.local"))
	if err != nil {
		slog.Error("invalid SMTP_FROM")
		os.Exit(1)
	}
	s := &auth.Server{DB: db, BaseURL: base, InternalKey: key, SecureCookies: auth.SecureCookies(base), Mailer: auth.SMTPMailer{
		Host: platform.Env("SMTP_HOST", "mailpit"), Port: platform.Env("SMTP_PORT", "1025"),
		Username: os.Getenv("SMTP_USERNAME"), Password: os.Getenv("SMTP_PASSWORD"), From: from,
		RequireTLS: platform.Env("SMTP_REQUIRE_TLS", "false") == "true",
	}}
	cleanup, stop := context.WithCancel(context.Background())
	defer stop()
	go s.Cleanup(cleanup)
	platform.Serve(platform.Env("LISTEN_ADDR", ":8081"), s.Handler())
}
