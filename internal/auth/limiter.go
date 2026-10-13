package auth

import (
	"net"
	"net/http"
	"sync"
	"time"

	"noteit/internal/platform"
)

type bucket struct {
	count int
	reset time.Time
}
type limiter struct {
	mu      sync.Mutex
	buckets map[string]bucket
}

func (l *limiter) wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The only public ingress is nginx, which overwrites X-Real-IP.
		ip := r.Header.Get("X-Real-IP")
		if ip == "" {
			ip, _, _ = net.SplitHostPort(r.RemoteAddr)
		}
		now := time.Now()
		l.mu.Lock()
		if l.buckets == nil {
			l.buckets = map[string]bucket{}
		}
		for key, b := range l.buckets {
			if now.After(b.reset) {
				delete(l.buckets, key)
			}
		}
		b := l.buckets[ip]
		allowed := b.count < 20 && len(l.buckets) < 10000
		if b.count == 0 {
			b.reset = now.Add(time.Minute)
		}
		if allowed {
			b.count++
			l.buckets[ip] = b
		}
		l.mu.Unlock()
		if !allowed {
			w.Header().Set("Retry-After", "60")
			platform.Error(w, 429, "Too many attempts. Try again in a minute.")
			return
		}
		next(w, r)
	}
}
