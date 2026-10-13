package platform

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeRejectsAmbiguousInput(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{"title":"ok","extra":true}`, `{"title":"ok"} {}`, `not json`, strings.Repeat("x", (1<<20)+1)} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		var input struct {
			Title string `json:"title"`
		}
		if Decode(w, r, &input) {
			t.Fatalf("accepted invalid JSON: %.30s", body)
		}
	}
}

func TestOriginProtection(t *testing.T) {
	h := Middleware("http://localhost:8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, test := range []struct {
		origin, site string
		code         int
	}{{"http://localhost:8080", "same-origin", 204}, {"https://attacker.example", "cross-site", 403}, {"", "cross-site", 403}, {"", "", 204}} {
		r := httptest.NewRequest("POST", "/", nil)
		r.Header.Set("Origin", test.origin)
		r.Header.Set("Sec-Fetch-Site", test.site)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.code {
			t.Fatalf("got %d want %d", w.Code, test.code)
		}
	}
}

func TestEmailAndToken(t *testing.T) {
	if value, err := Email(" User@Example.com "); err != nil || value != "user@example.com" {
		t.Fatal(value, err)
	}
	for _, value := range []string{"Name <a@example.com>", "a@example.com\r\nBcc: b@example.com", "no-at"} {
		if _, err := Email(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	token := Token()
	if !ValidToken(token) || Hash(token) == token || ValidToken("abc") {
		t.Fatal("token validation failed")
	}
}
