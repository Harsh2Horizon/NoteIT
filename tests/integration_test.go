package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"noteit/internal/auth"
	"noteit/internal/notes"
	"noteit/internal/platform"
)

type inbox struct {
	mu    sync.Mutex
	links map[string]string
	fail  bool
}

func (m *inbox) SendVerification(_ context.Context, email, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return fmt.Errorf("SMTP unavailable")
	}
	m.links[email] = link
	return nil
}
func (m *inbox) token(email string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.TrimPrefix(m.links[email], "http://localhost:8080/#verify=")
}

// Exercise the real inter-service HTTP client without needing a listening TCP port.
type handlerTransport struct{ handler http.Handler }

func (h handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	return w.Result(), nil
}

var requestID atomic.Int64

func request(h http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("X-Real-IP", fmt.Sprintf("test-%d", requestID.Add(1)))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func status(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String())
	}
}
func note(t *testing.T, w *httptest.ResponseRecorder) notes.Note {
	t.Helper()
	var n notes.Note
	if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestApplicationLifecycle(t *testing.T) {
	authURL, notesURL := os.Getenv("TEST_AUTH_DATABASE_URL"), os.Getenv("TEST_NOTES_DATABASE_URL")
	if authURL == "" || notesURL == "" {
		t.Skip("run make integration to test against temporary PostgreSQL databases")
	}
	ctx := context.Background()
	authDB, err := platform.OpenDB(ctx, authURL, auth.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()
	notesDB, err := platform.OpenDB(ctx, notesURL, notes.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	defer notesDB.Close()
	// Migrations must also work on a previously initialized volume.
	second, err := platform.OpenDB(ctx, notesURL, notes.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
	mail := &inbox{links: map[string]string{}}
	key := strings.Repeat("k", 64)
	authServer := &auth.Server{DB: authDB, Mailer: mail, BaseURL: "http://localhost:8080", InternalKey: key}
	authHTTP := authServer.Handler()
	backend := (&notes.Server{DB: notesDB, BaseURL: "http://localhost:8080", Auth: notes.AuthClient{URL: "http://auth", Key: key, Client: &http.Client{Transport: handlerTransport{authHTTP}}}}).Handler()
	suffix := fmt.Sprint(time.Now().UnixNano())
	ownerEmail, guestEmail, otherEmail := "owner"+suffix+"@example.com", "guest"+suffix+"@example.com", "other"+suffix+"@example.com"
	password := "correct horse battery staple"
	credentials := func(email string) map[string]any { return map[string]any{"email": email, "password": password} }
	register := func(email string) {
		t.Helper()
		status(t, request(authHTTP, "POST", "/api/auth/register", credentials(email), nil), 202)
	}
	verify := func(email string) {
		t.Helper()
		status(t, request(authHTTP, "POST", "/api/auth/verify", map[string]any{"token": mail.token(email)}, nil), 200)
	}
	login := func(email string) *http.Cookie {
		t.Helper()
		w := request(authHTTP, "POST", "/api/auth/login", credentials(email), nil)
		status(t, w, 200)
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
			t.Fatal("unsafe or missing session cookie")
		}
		return cookies[0]
	}
	var owner, guest, other *http.Cookie
	t.Run("verification and sessions", func(t *testing.T) {
		register(ownerEmail)
		status(t, request(authHTTP, "POST", "/api/auth/login", credentials(ownerEmail), nil), 403)
		status(t, request(authHTTP, "POST", "/api/auth/verify", map[string]any{"token": strings.Repeat("0", 64)}, nil), 400)
		token := mail.token(ownerEmail)
		verify(ownerEmail)
		status(t, request(authHTTP, "POST", "/api/auth/verify", map[string]any{"token": token}, nil), 400)
		owner = login(ownerEmail)
		status(t, request(authHTTP, "GET", "/api/auth/me", nil, owner), 200)
		status(t, request(authHTTP, "GET", "/internal/session", nil, owner), 401)
		status(t, request(authHTTP, "POST", "/api/auth/login", map[string]any{"email": ownerEmail, "password": "wrong"}, nil), 401)
		register(guestEmail)
		verify(guestEmail)
		guest = login(guestEmail)
		register(otherEmail)
		verify(otherEmail)
		other = login(otherEmail)
		var stored string
		if err := authDB.QueryRow(ctx, "SELECT token_hash FROM sessions WHERE token_hash=$1", platform.Hash(owner.Value)).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored == owner.Value {
			t.Fatal("raw session token persisted")
		}
	})
	if owner == nil || guest == nil || other == nil {
		t.Fatal("account setup failed")
	}
	var n notes.Note
	t.Run("create and owner isolation", func(t *testing.T) {
		status(t, request(backend, "POST", "/api/notes", map[string]any{}, nil), 401)
		w := request(backend, "POST", "/api/notes", map[string]any{"title": "Ideas", "content": "# PostgreSQL notebook\n\n- First idea", "tags": []string{" Work ", "work", "Ideas"}}, owner)
		status(t, w, 201)
		n = note(t, w)
		if len(n.Tags) != 2 || n.Tags[0] != "work" || n.Version != 1 {
			t.Fatal("note defaults or tags incorrect")
		}
		status(t, request(backend, "GET", "/api/notes/"+n.ID, nil, other), 404)
		status(t, request(backend, "PATCH", "/api/notes/"+n.ID, map[string]any{"version": 1, "title": "stolen"}, other), 404)
		status(t, request(backend, "DELETE", "/api/notes/"+n.ID, map[string]any{"version": 1}, other), 404)
		status(t, request(backend, "PUT", "/api/notes/"+n.ID+"/publication", map[string]any{"version": 1, "visibility": "public"}, other), 404)
		status(t, request(backend, "GET", "/api/notes/not-a-uuid", nil, owner), 404)
		w = request(backend, "GET", "/api/notes", nil, other)
		status(t, w, 200)
		if !strings.Contains(w.Body.String(), `"total":0`) {
			t.Fatal("notes leaked across owners")
		}
	})
	if n.ID == "" {
		t.Fatal("note setup failed")
	}
	patch := func(body map[string]any) notes.Note {
		t.Helper()
		body["version"] = n.Version
		w := request(backend, "PATCH", "/api/notes/"+n.ID, body, owner)
		status(t, w, 200)
		return note(t, w)
	}
	pub := func(visibility string, emails []string) notes.Note {
		t.Helper()
		w := request(backend, "PUT", "/api/notes/"+n.ID+"/publication", map[string]any{"version": n.Version, "visibility": visibility, "allowed_emails": emails}, owner)
		status(t, w, 200)
		return note(t, w)
	}
	sharedPath := func(n notes.Note) string {
		t.Helper()
		if n.ShareURL == nil {
			t.Fatal("missing share URL")
		}
		return "/api/published/" + strings.TrimPrefix(*n.ShareURL, "http://localhost:8080/s/")
	}
	t.Run("autosave and concurrent updates", func(t *testing.T) {
		n = patch(map[string]any{"title": "Updated", "pinned": true})
		w := request(backend, "PATCH", "/api/notes/"+n.ID, map[string]any{"version": 1, "title": "stale"}, owner)
		status(t, w, 409)
		if !strings.Contains(w.Body.String(), `"current"`) {
			t.Fatal("conflict lacks current note")
		}
		var wg sync.WaitGroup
		results := make(chan *httptest.ResponseRecorder, 2)
		for _, title := range []string{"tab one", "tab two"} {
			wg.Add(1)
			go func(title string) {
				defer wg.Done()
				results <- request(backend, "PATCH", "/api/notes/"+n.ID, map[string]any{"version": n.Version, "title": title}, owner)
			}(title)
		}
		wg.Wait()
		close(results)
		success, conflict := 0, 0
		for w := range results {
			switch w.Code {
			case 200:
				success++
				n = note(t, w)
			case 409:
				conflict++
			default:
				t.Fatalf("unexpected concurrent response: %s", w.Body.String())
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatal("lost-update protection failed")
		}
		status(t, request(backend, "PATCH", "/api/notes/"+n.ID, map[string]any{"version": n.Version, "content": strings.Repeat("x", 200001)}, owner), 400)
		status(t, request(backend, "PATCH", "/api/notes/"+n.ID, map[string]any{"title": "no version"}, owner), 400)
	})
	t.Run("search tags and pagination", func(t *testing.T) {
		for _, path := range []string{"/api/notes?q=PostgreSQL&tag=work&pinned=true", "/api/notes?q=ideas", "/api/notes?limit=1&offset=0"} {
			w := request(backend, "GET", path, nil, owner)
			status(t, w, 200)
			if !strings.Contains(w.Body.String(), `"total":1`) {
				t.Fatalf("search missed note: %s", w.Body.String())
			}
		}
		w := request(backend, "GET", "/api/tags", nil, owner)
		status(t, w, 200)
		if !strings.Contains(w.Body.String(), `"name":"work"`) {
			t.Fatal("tag missing")
		}
		status(t, request(backend, "GET", "/api/notes?limit=500", nil, owner), 400)
		status(t, request(backend, "GET", "/api/notes?state=bad", nil, owner), 400)
	})
	t.Run("private restricted and public publishing", func(t *testing.T) {
		n = pub("private", nil)
		path := sharedPath(n)
		status(t, request(backend, "GET", path, nil, nil), 401)
		status(t, request(backend, "GET", path, nil, owner), 200)
		status(t, request(backend, "GET", path, nil, guest), 404)
		n = pub("restricted", []string{strings.ToUpper(guestEmail)})
		status(t, request(backend, "GET", path, nil, guest), 200)
		status(t, request(backend, "GET", path, nil, other), 404)
		status(t, request(backend, "GET", path, nil, nil), 401)
		n = pub("restricted", []string{})
		status(t, request(backend, "GET", path, nil, guest), 404)
		n = pub("public", nil)
		w := request(backend, "GET", path, nil, nil)
		status(t, w, 200)
		for _, secret := range []string{"allowed_emails", "owner_id", "tags", "version"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("published response leaked " + secret)
			}
		}
		n = patch(map[string]any{"content": "# Live published content"})
		w = request(backend, "GET", path, nil, nil)
		status(t, w, 200)
		if !strings.Contains(w.Body.String(), "Live published content") {
			t.Fatal("publication is not live")
		}
		w = request(backend, "DELETE", "/api/notes/"+n.ID+"/publication", map[string]any{"version": n.Version}, owner)
		status(t, w, 200)
		n = note(t, w)
		status(t, request(backend, "GET", path, nil, nil), 404)
		n = pub("public", nil)
		if sharedPath(n) == path {
			t.Fatal("republish reused revoked token")
		}
	})
	t.Run("archive trash restore revoke links", func(t *testing.T) {
		oldPath := sharedPath(n)
		n = patch(map[string]any{"archived": true})
		status(t, request(backend, "GET", oldPath, nil, nil), 404)
		status(t, request(backend, "PUT", "/api/notes/"+n.ID+"/publication", map[string]any{"version": n.Version, "visibility": "public"}, owner), 409)
		w := request(backend, "GET", "/api/notes?state=archived", nil, owner)
		status(t, w, 200)
		if !strings.Contains(w.Body.String(), `"total":1`) {
			t.Fatal("archive filter")
		}
		n = patch(map[string]any{"archived": false})
		if n.ShareURL != nil {
			t.Fatal("unarchiving restored a revoked link")
		}
		n = pub("public", nil)
		oldPath = sharedPath(n)
		w = request(backend, "DELETE", "/api/notes/"+n.ID, map[string]any{"version": n.Version}, owner)
		status(t, w, 200)
		n = note(t, w)
		status(t, request(backend, "GET", oldPath, nil, nil), 404)
		status(t, request(backend, "PATCH", "/api/notes/"+n.ID, map[string]any{"version": n.Version, "title": "cannot edit trash"}, owner), 409)
		w = request(backend, "GET", "/api/notes?state=trash", nil, owner)
		status(t, w, 200)
		if !strings.Contains(w.Body.String(), `"total":1`) {
			t.Fatal("trash filter")
		}
		w = request(backend, "GET", "/api/notes", nil, owner)
		status(t, w, 200)
		if !strings.Contains(w.Body.String(), `"total":0`) {
			t.Fatal("deleted note still active")
		}
		w = request(backend, "POST", "/api/notes/"+n.ID+"/restore", map[string]any{"version": n.Version}, owner)
		status(t, w, 200)
		n = note(t, w)
		if n.DeletedAt != nil || n.Archived || n.ShareURL != nil {
			t.Fatal("restore state")
		}
	})
	t.Run("verification expiry resend and SMTP rollback", func(t *testing.T) {
		email := "expiring" + suffix + "@example.com"
		register(email)
		token := mail.token(email)
		if _, err := authDB.Exec(ctx, `UPDATE verification_tokens SET expires_at=now()-interval '1 minute',created_at=now()-interval '2 minutes' WHERE token_hash=$1`, platform.Hash(token)); err != nil {
			t.Fatal(err)
		}
		status(t, request(authHTTP, "POST", "/api/auth/verify", map[string]any{"token": token}, nil), 400)
		status(t, request(authHTTP, "POST", "/api/auth/resend-verification", map[string]any{"email": email}, nil), 202)
		if mail.token(email) == token {
			t.Fatal("resend did not rotate token")
		}
		verify(email)
		mail.fail = true
		failedEmail := "failed" + suffix + "@example.com"
		status(t, request(authHTTP, "POST", "/api/auth/register", credentials(failedEmail), nil), 503)
		mail.fail = false
		var count int
		if err := authDB.QueryRow(ctx, "SELECT count(*) FROM users WHERE email=$1", failedEmail).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed signup was not rolled back", err)
		}
	})
	t.Run("logout expiry csrf and rate limit", func(t *testing.T) {
		n = pub("restricted", []string{guestEmail})
		status(t, request(authHTTP, "POST", "/api/auth/logout", map[string]any{}, guest), 200)
		status(t, request(backend, "GET", sharedPath(n), nil, guest), 401)
		if _, err := authDB.Exec(ctx, "UPDATE sessions SET expires_at=now()-interval '1 second' WHERE token_hash=$1", platform.Hash(other.Value)); err != nil {
			t.Fatal(err)
		}
		status(t, request(backend, "GET", "/api/notes", nil, other), 401)
		r := httptest.NewRequest("POST", "/api/notes", strings.NewReader(`{}`))
		r.AddCookie(owner)
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		backend.ServeHTTP(w, r)
		status(t, w, 403)
		for i := 0; i < 21; i++ {
			r = httptest.NewRequest("POST", "/api/auth/register", strings.NewReader(`{}`))
			r.Header.Set("X-Real-IP", "rate-test")
			r.Header.Set("Content-Type", "application/json")
			w = httptest.NewRecorder()
			authHTTP.ServeHTTP(w, r)
		}
		status(t, w, 429)
	})
}
