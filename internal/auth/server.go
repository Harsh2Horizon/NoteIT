package auth

import (
	"context"
	"crypto/subtle"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"noteit/internal/platform"
)

//go:embed migrations/*.sql
var Migrations embed.FS

type Server struct {
	DB                   *pgxpool.Pool
	Mailer               Mailer
	BaseURL, InternalKey string
	SecureCookies        bool
	dummyHash            string
}

func (s *Server) Handler() http.Handler {
	s.dummyHash, _ = HashPassword(platform.Token())
	mux := http.NewServeMux()
	l := &limiter{}
	mux.HandleFunc("GET /healthz", platform.Health(s.DB))
	mux.HandleFunc("POST /api/auth/register", l.wrap(s.register))
	mux.HandleFunc("POST /api/auth/login", l.wrap(s.login))
	mux.HandleFunc("POST /api/auth/verify", l.wrap(s.verify))
	mux.HandleFunc("POST /api/auth/resend-verification", l.wrap(s.resend))
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.me)
	mux.HandleFunc("GET /internal/session", s.internalSession)
	return platform.Middleware(s.BaseURL, mux)
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

const sentMessage = "If this email is eligible, a verification link has been sent. Check your inbox."

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var input credentials
	if !platform.Decode(w, r, &input) {
		return
	}
	email, err := platform.Email(input.Email)
	if err != nil {
		platform.Error(w, 400, err.Error())
		return
	}
	if utf8.RuneCountInString(input.Password) < 12 || len(input.Password) > 128 {
		platform.Error(w, 400, "Password must be at least 12 characters and at most 128 bytes")
		return
	}
	hash, err := HashPassword(input.Password)
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO users(email,password_hash) VALUES($1,$2) ON CONFLICT(email) DO NOTHING RETURNING id::text`, email, hash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		platform.JSON(w, 202, map[string]string{"message": sentMessage})
		return
	}
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	if err = s.sendToken(r.Context(), tx, id, email); err != nil {
		slog.Error("verification delivery failed", "error", err)
		platform.Error(w, 503, "Email delivery failed. Please try again shortly.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		platform.InternalError(w, err)
		return
	}
	platform.JSON(w, 202, map[string]string{"message": sentMessage})
}

func (s *Server) sendToken(ctx context.Context, tx pgx.Tx, id, email string) error {
	token := platform.Token()
	if _, err := tx.Exec(ctx, "DELETE FROM verification_tokens WHERE user_id=$1", id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO verification_tokens(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '30 minutes')`, platform.Hash(token), id); err != nil {
		return err
	}
	return s.Mailer.SendVerification(ctx, email, s.BaseURL+"/#verify="+token)
}

func (s *Server) resend(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email string `json:"email"`
	}
	if !platform.Decode(w, r, &input) {
		return
	}
	email, err := platform.Email(input.Email)
	if err != nil {
		platform.Error(w, 400, err.Error())
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `SELECT id::text FROM users WHERE email=$1 AND verified_at IS NULL FOR UPDATE`, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		platform.JSON(w, 202, map[string]string{"message": sentMessage})
		return
	}
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	var recent bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM verification_tokens WHERE user_id=$1 AND created_at > now()-interval '1 minute')`, id).Scan(&recent); err != nil {
		platform.InternalError(w, err)
		return
	}
	if !recent {
		if err = s.sendToken(r.Context(), tx, id, email); err != nil {
			slog.Error("verification delivery failed", "error", err)
			platform.Error(w, 503, "Email delivery failed. Please try again shortly.")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		platform.InternalError(w, err)
		return
	}
	platform.JSON(w, 202, map[string]string{"message": sentMessage})
}

func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !platform.Decode(w, r, &input) {
		return
	}
	if !platform.ValidToken(input.Token) {
		platform.Error(w, 400, "Invalid or expired verification link")
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	// Lock the user first, matching resend's lock order and preventing concurrent token reuse.
	var id string
	err = tx.QueryRow(r.Context(), `SELECT u.id::text FROM users u JOIN verification_tokens v ON v.user_id=u.id WHERE v.token_hash=$1 AND v.expires_at>now() AND u.verified_at IS NULL FOR UPDATE OF u`, platform.Hash(input.Token)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		platform.Error(w, 400, "Invalid or expired verification link")
		return
	}
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	result, err := tx.Exec(r.Context(), `DELETE FROM verification_tokens WHERE token_hash=$1 AND expires_at>now()`, platform.Hash(input.Token))
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	if result.RowsAffected() != 1 {
		platform.Error(w, 400, "Invalid or expired verification link")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET verified_at=now() WHERE id=$1`, id); err != nil {
		platform.InternalError(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM verification_tokens WHERE user_id=$1`, id); err != nil {
		platform.InternalError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		platform.InternalError(w, err)
		return
	}
	platform.JSON(w, 200, map[string]string{"message": "Email verified. You can now sign in."})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input credentials
	if !platform.Decode(w, r, &input) {
		return
	}
	email, _ := platform.Email(input.Email)
	if len(input.Password) > 128 {
		platform.Error(w, 401, "Invalid email or password")
		return
	}
	var user platform.User
	var hash string
	var verified bool
	err := s.DB.QueryRow(r.Context(), `SELECT id::text,email,password_hash,verified_at IS NOT NULL FROM users WHERE email=$1`, email).Scan(&user.ID, &user.Email, &hash, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = CheckPassword(s.dummyHash, input.Password)
		platform.Error(w, 401, "Invalid email or password")
		return
	}
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	if !CheckPassword(hash, input.Password) {
		platform.Error(w, 401, "Invalid email or password")
		return
	}
	if !verified {
		platform.Error(w, 403, "Verify your email before signing in")
		return
	}
	token := platform.Token()
	if _, err = s.DB.Exec(r.Context(), `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '7 days')`, platform.Hash(token), user.ID); err != nil {
		platform.InternalError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: platform.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 60 * 60})
	platform.JSON(w, 200, user)
}

func (s *Server) session(r *http.Request) (platform.User, error) {
	var user platform.User
	cookie, err := r.Cookie(platform.CookieName)
	if err != nil || !platform.ValidToken(cookie.Value) {
		return user, pgx.ErrNoRows
	}
	err = s.DB.QueryRow(r.Context(), `SELECT u.id::text,u.email FROM sessions s JOIN users u ON s.user_id=u.id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.verified_at IS NOT NULL`, platform.Hash(cookie.Value)).Scan(&user.ID, &user.Email)
	return user, err
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user, err := s.session(r)
	if errors.Is(err, pgx.ErrNoRows) {
		platform.Error(w, 401, "Please sign in")
		return
	}
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	platform.JSON(w, 200, user)
}

func (s *Server) internalSession(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-Internal-Key")
	if len(s.InternalKey) < 32 || subtle.ConstantTimeCompare([]byte(key), []byte(s.InternalKey)) != 1 {
		platform.Error(w, 401, "Unauthorized")
		return
	}
	s.me(w, r)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !platform.Decode(w, r, &input) {
		return
	}
	if cookie, err := r.Cookie(platform.CookieName); err == nil {
		if _, err = s.DB.Exec(r.Context(), "DELETE FROM sessions WHERE token_hash=$1", platform.Hash(cookie.Value)); err != nil {
			platform.InternalError(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: platform.CookieName, Value: "", Path: "/", HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	platform.JSON(w, 200, map[string]string{"message": "Signed out"})
}

func (s *Server) Cleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		for _, table := range []string{"sessions", "verification_tokens"} {
			if _, err := s.DB.Exec(ctx, "DELETE FROM "+table+" WHERE expires_at<now()"); err != nil && ctx.Err() == nil {
				slog.Error("auth cleanup failed", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func SecureCookies(baseURL string) bool { return strings.HasPrefix(baseURL, "https://") }
