package notes

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"noteit/internal/platform"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAuthClientFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name                        string
		code                        int
		body                        string
		wantUnauthorized, wantError bool
	}{
		{"verified user", 200, `{"id":"user-id","email":"user@example.com"}`, false, false},
		{"expired session", 401, `{}`, true, true},
		{"auth down", 503, `{}`, false, true},
		{"bad response", 200, `not JSON`, false, true},
		{"incomplete identity", 200, `{"id":"user-id"}`, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := AuthClient{URL: "http://auth", Key: "internal-key", Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("X-Internal-Key") != "internal-key" {
					t.Error("missing internal credential")
				}
				if _, err := r.Cookie(platform.CookieName); err != nil {
					t.Error("missing forwarded session")
				}
				return &http.Response{StatusCode: test.code, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}, nil
			})}}
			r := httptest.NewRequest("GET", "/", nil)
			r.AddCookie(&http.Cookie{Name: platform.CookieName, Value: platform.Token()})
			_, err := client.Authenticate(r)
			if (err != nil) != test.wantError || errors.Is(err, ErrUnauthorized) != test.wantUnauthorized {
				t.Fatalf("unexpected auth result: %v", err)
			}
		})
	}
	client := AuthClient{}
	if _, err := client.Authenticate(httptest.NewRequest("GET", "/", nil)); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("anonymous request allowed")
	}
}
