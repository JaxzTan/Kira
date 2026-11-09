package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// rateLimiter allows `perMinute` requests per minute per client IP as a
// token bucket (burst = perMinute, refill one token every minute/perMinute).
type rateLimiter struct {
	perMinute int
	now       func() time.Time

	mu      sync.Mutex
	clients map[string]*client
	calls   int
}

type client struct {
	lim  *rate.Limiter
	seen time.Time
}

func newRateLimiter(perMinute int, now func() time.Time) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{perMinute: perMinute, now: now, clients: map[string]*client{}}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	rl.calls++
	if rl.calls%256 == 0 { // occasional sweep of idle clients
		for k, c := range rl.clients {
			if now.Sub(c.seen) > 2*time.Minute {
				delete(rl.clients, k)
			}
		}
	}
	c, ok := rl.clients[ip]
	if !ok {
		c = &client{lim: rate.NewLimiter(rate.Every(time.Minute/time.Duration(rl.perMinute)), rl.perMinute)}
		rl.clients[ip] = c
	}
	c.seen = now
	return c.lim.AllowN(now, 1)
}

func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, CodeRateLimited, "too many requests; try again in a minute", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the host part of r.RemoteAddr. X-Forwarded-For is trusted only
// when the direct peer is loopback (a local reverse proxy such as
// `tailscale serve` or the Vite dev proxy); then the right-most valid entry,
// i.e. the address that proxy itself saw, is used. A remote client cannot
// spoof its key because its own RemoteAddr is never loopback.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return host
	}
	xff := r.Header.Values("X-Forwarded-For")
	for i := len(xff) - 1; i >= 0; i-- {
		parts := strings.Split(xff[i], ",")
		for j := len(parts) - 1; j >= 0; j-- {
			if p := net.ParseIP(strings.TrimSpace(parts[j])); p != nil {
				return p.String()
			}
		}
	}
	return host
}
