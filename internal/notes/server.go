package notes

import (
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"noteit/internal/platform"
)

//go:embed migrations/*.sql
var Migrations embed.FS

type Server struct {
	DB      *pgxpool.Pool
	Auth    Authenticator
	BaseURL string
}
type ownerHandler func(http.ResponseWriter, *http.Request, platform.User)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", platform.Health(s.DB))
	mux.HandleFunc("GET /api/notes", s.owner(s.list))
	mux.HandleFunc("POST /api/notes", s.owner(s.create))
	mux.HandleFunc("GET /api/tags", s.owner(s.tags))
	mux.HandleFunc("GET /api/notes/{id}", s.owner(s.get))
	mux.HandleFunc("PATCH /api/notes/{id}", s.owner(s.update))
	mux.HandleFunc("DELETE /api/notes/{id}", s.owner(s.trash))
	mux.HandleFunc("POST /api/notes/{id}/restore", s.owner(s.restore))
	mux.HandleFunc("PUT /api/notes/{id}/publication", s.owner(s.publish))
	mux.HandleFunc("DELETE /api/notes/{id}/publication", s.owner(s.unpublish))
	mux.HandleFunc("GET /api/published/{token}", s.shared)
	return platform.Middleware(s.BaseURL, mux)
}

func (s *Server) owner(next ownerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.Auth.Authenticate(r)
		if errors.Is(err, ErrUnauthorized) {
			platform.Error(w, 401, "Please sign in")
			return
		}
		if err != nil {
			slog.Error("authentication unavailable", "error", err)
			platform.Error(w, 503, "Authentication is temporarily unavailable")
			return
		}
		if id := r.PathValue("id"); id != "" && !uuidPattern.MatchString(id) {
			platform.Error(w, 404, "Note not found")
			return
		}
		next(w, r, user)
	}
}

func (s *Server) list(w http.ResponseWriter, r *http.Request, u platform.User) {
	q := r.URL.Query()
	state := q.Get("state")
	if state == "" {
		state = "active"
	}
	if state != "active" && state != "archived" && state != "trash" {
		platform.Error(w, 400, "State must be active, archived, or trash")
		return
	}
	search := strings.TrimSpace(q.Get("q"))
	if len(search) > 200 {
		platform.Error(w, 400, "Search is too long")
		return
	}
	limit, offset := 50, 0
	var err error
	if q.Has("limit") {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			platform.Error(w, 400, "Limit must be between 1 and 100")
			return
		}
	}
	if q.Has("offset") {
		offset, err = strconv.Atoi(q.Get("offset"))
		if err != nil || offset < 0 || offset > 100000 {
			platform.Error(w, 400, "Invalid offset")
			return
		}
	}
	if q.Has("pinned") && q.Get("pinned") != "true" && q.Get("pinned") != "false" {
		platform.Error(w, 400, "Pinned must be true or false")
		return
	}
	where := ` WHERE owner_id=$1 AND (($2='trash' AND deleted_at IS NOT NULL) OR ($2='archived' AND deleted_at IS NULL AND archived) OR ($2='active' AND deleted_at IS NULL AND NOT archived))
        AND ($3='' OR search_document @@ websearch_to_tsquery('simple',$3) OR strpos(lower(title),lower($3))>0 OR EXISTS(SELECT 1 FROM unnest(tags) t WHERE strpos(t,lower($3))>0))
        AND ($4='' OR tags @> ARRAY[$4]::text[]) AND ($5='' OR pinned=($5='true'))`
	args := []any{u.ID, state, search, strings.ToLower(strings.TrimSpace(q.Get("tag"))), q.Get("pinned")}
	var total int
	if err = s.DB.QueryRow(r.Context(), "SELECT count(*) FROM notes"+where, args...).Scan(&total); err != nil {
		platform.InternalError(w, err)
		return
	}
	args = append(args, limit, offset)
	rows, err := s.DB.Query(r.Context(), "SELECT "+summaryColumns+" FROM notes"+where+" ORDER BY pinned DESC,updated_at DESC,id LIMIT $6 OFFSET $7", args...)
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	defer rows.Close()
	notes := []Note{}
	for rows.Next() {
		n, err := scan(rows, true, s.BaseURL)
		if err != nil {
			platform.InternalError(w, err)
			return
		}
		notes = append(notes, n)
	}
	if err = rows.Err(); err != nil {
		platform.InternalError(w, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"notes": notes, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) tags(w http.ResponseWriter, r *http.Request, u platform.User) {
	rows, err := s.DB.Query(r.Context(), `SELECT tag,count(*) FROM notes CROSS JOIN LATERAL unnest(tags) tag WHERE owner_id=$1 AND deleted_at IS NULL GROUP BY tag ORDER BY tag`, u.ID)
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	defer rows.Close()
	type tag struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	tags := []tag{}
	for rows.Next() {
		var t tag
		if err = rows.Scan(&t.Name, &t.Count); err != nil {
			platform.InternalError(w, err)
			return
		}
		tags = append(tags, t)
	}
	if err = rows.Err(); err != nil {
		platform.InternalError(w, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"tags": tags})
}

func (s *Server) get(w http.ResponseWriter, r *http.Request, u platform.User) {
	n, err := scan(s.DB.QueryRow(r.Context(), "SELECT "+columns+" FROM notes WHERE id=$1 AND owner_id=$2", r.PathValue("id"), u.ID), false, s.BaseURL)
	if errors.Is(err, pgx.ErrNoRows) {
		platform.Error(w, 404, "Note not found")
		return
	}
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	platform.JSON(w, 200, n)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request, u platform.User) {
	var in struct {
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if !platform.Decode(w, r, &in) {
		return
	}
	if err := validateText(in.Title, in.Content); err != nil {
		platform.Error(w, 400, err.Error())
		return
	}
	tags, err := normalizeTags(in.Tags)
	if err != nil {
		platform.Error(w, 400, err.Error())
		return
	}
	n, err := scan(s.DB.QueryRow(r.Context(), "INSERT INTO notes(owner_id,title,content,tags) VALUES($1,$2,$3,$4) RETURNING "+columns, u.ID, in.Title, in.Content, tags), false, s.BaseURL)
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	platform.JSON(w, 201, n)
}

type patch struct {
	Version  int64     `json:"version"`
	Title    *string   `json:"title"`
	Content  *string   `json:"content"`
	Tags     *[]string `json:"tags"`
	Pinned   *bool     `json:"pinned"`
	Archived *bool     `json:"archived"`
}

func (s *Server) update(w http.ResponseWriter, r *http.Request, u platform.User) {
	var in patch
	if !platform.Decode(w, r, &in) {
		return
	}
	if in.Version < 1 {
		platform.Error(w, 400, "A positive version is required")
		return
	}
	var title, content string
	if in.Title != nil {
		title = *in.Title
	}
	if in.Content != nil {
		content = *in.Content
	}
	if err := validateText(title, content); err != nil {
		platform.Error(w, 400, err.Error())
		return
	}
	var tags any
	if in.Tags != nil {
		normalized, err := normalizeTags(*in.Tags)
		if err != nil {
			platform.Error(w, 400, err.Error())
			return
		}
		tags = normalized
	}
	n, err := scan(s.DB.QueryRow(r.Context(), `UPDATE notes SET title=coalesce($4,title),content=coalesce($5,content),tags=coalesce($6,tags),pinned=coalesce($7,pinned),archived=coalesce($8,archived),
        share_token=CASE WHEN $8=true THEN NULL ELSE share_token END,
        visibility=CASE WHEN $8=true THEN 'private' ELSE visibility END,
        allowed_emails=CASE WHEN $8=true THEN '{}'::text[] ELSE allowed_emails END,
        version=version+1,updated_at=now()
        WHERE id=$1 AND owner_id=$2 AND version=$3 AND deleted_at IS NULL RETURNING `+columns, r.PathValue("id"), u.ID, in.Version, in.Title, in.Content, tags, in.Pinned, in.Archived), false, s.BaseURL)
	s.mutationResponse(w, r, u, n, err)
}

func (s *Server) mutationResponse(w http.ResponseWriter, r *http.Request, u platform.User, n Note, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		current, e := scan(s.DB.QueryRow(r.Context(), "SELECT "+columns+" FROM notes WHERE id=$1 AND owner_id=$2", r.PathValue("id"), u.ID), false, s.BaseURL)
		if errors.Is(e, pgx.ErrNoRows) {
			platform.Error(w, 404, "Note not found")
			return
		}
		if e != nil {
			platform.InternalError(w, e)
			return
		}
		platform.JSON(w, 409, map[string]any{"error": "This note changed or this action is unavailable in its current state. Reload before continuing.", "current": current})
		return
	}
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	platform.JSON(w, 200, n)
}

func versionInput(w http.ResponseWriter, r *http.Request) (int64, bool) {
	var in struct {
		Version int64 `json:"version"`
	}
	if !platform.Decode(w, r, &in) {
		return 0, false
	}
	if in.Version < 1 {
		platform.Error(w, 400, "A positive version is required")
		return 0, false
	}
	return in.Version, true
}

func (s *Server) trash(w http.ResponseWriter, r *http.Request, u platform.User) {
	v, ok := versionInput(w, r)
	if !ok {
		return
	}
	n, err := scan(s.DB.QueryRow(r.Context(), `UPDATE notes SET deleted_at=now(),pinned=false,share_token=NULL,visibility='private',allowed_emails='{}',version=version+1,updated_at=now() WHERE id=$1 AND owner_id=$2 AND version=$3 AND deleted_at IS NULL RETURNING `+columns, r.PathValue("id"), u.ID, v), false, s.BaseURL)
	s.mutationResponse(w, r, u, n, err)
}

func (s *Server) restore(w http.ResponseWriter, r *http.Request, u platform.User) {
	v, ok := versionInput(w, r)
	if !ok {
		return
	}
	n, err := scan(s.DB.QueryRow(r.Context(), `UPDATE notes SET deleted_at=NULL,archived=false,version=version+1,updated_at=now() WHERE id=$1 AND owner_id=$2 AND version=$3 AND deleted_at IS NOT NULL RETURNING `+columns, r.PathValue("id"), u.ID, v), false, s.BaseURL)
	s.mutationResponse(w, r, u, n, err)
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request, u platform.User) {
	var in struct {
		Version       int64    `json:"version"`
		Visibility    string   `json:"visibility"`
		AllowedEmails []string `json:"allowed_emails"`
	}
	if !platform.Decode(w, r, &in) {
		return
	}
	if in.Version < 1 {
		platform.Error(w, 400, "A positive version is required")
		return
	}
	emails, err := normalizeAccess(in.Visibility, in.AllowedEmails)
	if err != nil {
		platform.Error(w, 400, err.Error())
		return
	}
	n, err := scan(s.DB.QueryRow(r.Context(), `UPDATE notes SET visibility=$4,allowed_emails=$5,share_token=coalesce(share_token,$6),version=version+1,updated_at=now() WHERE id=$1 AND owner_id=$2 AND version=$3 AND deleted_at IS NULL AND NOT archived RETURNING `+columns, r.PathValue("id"), u.ID, in.Version, in.Visibility, emails, platform.Token()), false, s.BaseURL)
	s.mutationResponse(w, r, u, n, err)
}

func (s *Server) unpublish(w http.ResponseWriter, r *http.Request, u platform.User) {
	v, ok := versionInput(w, r)
	if !ok {
		return
	}
	n, err := scan(s.DB.QueryRow(r.Context(), `UPDATE notes SET visibility='private',allowed_emails='{}',share_token=NULL,version=version+1,updated_at=now() WHERE id=$1 AND owner_id=$2 AND version=$3 AND deleted_at IS NULL RETURNING `+columns, r.PathValue("id"), u.ID, v), false, s.BaseURL)
	s.mutationResponse(w, r, u, n, err)
}

func (s *Server) shared(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !platform.ValidToken(token) {
		platform.Error(w, 404, "Published note not found")
		return
	}
	var ownerID string
	n, err := scan(s.DB.QueryRow(r.Context(), "SELECT "+columns+" FROM notes WHERE share_token=$1 AND deleted_at IS NULL AND NOT archived", token), false, s.BaseURL)
	if errors.Is(err, pgx.ErrNoRows) {
		platform.Error(w, 404, "Published note not found")
		return
	}
	if err != nil {
		platform.InternalError(w, err)
		return
	}
	if n.Visibility != "public" {
		u, err := s.Auth.Authenticate(r)
		if errors.Is(err, ErrUnauthorized) {
			platform.Error(w, 401, "Sign in with an allowed, verified email to view this note")
			return
		}
		if err != nil {
			platform.Error(w, 503, "Authentication is temporarily unavailable")
			return
		}
		// Fetch owner and access in one fresh statement: revocation is checked on every request.
		err = s.DB.QueryRow(r.Context(), `SELECT owner_id::text FROM notes WHERE id=$1 AND share_token=$2 AND deleted_at IS NULL AND NOT archived AND (owner_id=$3 OR visibility='public' OR (visibility='restricted' AND $4=ANY(allowed_emails)))`, n.ID, token, u.ID, u.Email).Scan(&ownerID)
		if errors.Is(err, pgx.ErrNoRows) {
			platform.Error(w, 404, "Published note not found or access is not allowed")
			return
		}
		if err != nil {
			platform.InternalError(w, err)
			return
		}
	}
	// Published readers never receive the owner ID, tags, allowlist, or editing metadata.
	platform.JSON(w, 200, map[string]any{"title": n.Title, "content": n.Content, "updated_at": n.UpdatedAt})
}
