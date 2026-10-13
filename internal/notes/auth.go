package notes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"noteit/internal/platform"
)

var ErrUnauthorized = errors.New("unauthorized")

type Authenticator interface {
	Authenticate(*http.Request) (platform.User, error)
}

type AuthClient struct {
	URL, Key string
	Client   *http.Client
}

func (a AuthClient) Authenticate(r *http.Request) (platform.User, error) {
	var user platform.User

	cookie, err := r.Cookie(platform.CookieName)

	if err != nil || !platform.ValidToken(cookie.Value) {
		return user, ErrUnauthorized
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", a.URL+"/internal/session", nil)
	if err != nil {
		return user, err
	}
	req.AddCookie(cookie)
	req.Header.Set("X-Internal-Key", a.Key)
	res, err := a.Client.Do(req)
	if err != nil {
		return user, err
	}
	defer res.Body.Close()
	if res.StatusCode == 401 {
		return user, ErrUnauthorized
	}
	if res.StatusCode != 200 {
		return user, fmt.Errorf("auth service returned %d", res.StatusCode)
	}
	if err = json.NewDecoder(res.Body).Decode(&user); err != nil {
		return user, err
	}
	if user.ID == "" || user.Email == "" {
		return user, fmt.Errorf("auth service returned incomplete user")
	}
	return user, nil
}
