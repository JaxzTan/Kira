package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// maxRateClients caps the per-IP map so a flood of distinct addresses
// cannot grow memory without bound; the least recently seen client is
// evicted when it is full.
const maxRateClients = 10_000

// rateLimiter allows `perMinute` requests per minute per client IP as a
// token bucket (burst = perMinute, refill one token every minute/perMinute).
type rateLimiter struct {
	perMinute  int
	now        func() time.Time
	trustProxy bool
	max        int

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
	return &rateLimiter{perMinute: perMinute, now: now, max: maxRateClients, clients: map[string]*client{}}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	rl.calls++
	if rl.calls%256 == 0 { // occasional sweep of idle clients
		rl.sweepLocked(now)
	}
	c, ok := rl.clients[ip]
	if !ok {
		if len(rl.clients) >= rl.max {
			rl.sweepLocked(now)
		}
		for len(rl.clients) >= rl.max {
			rl.evictOldestLocked()
		}
		c = &client{lim: rate.NewLimiter(rate.Every(time.Minute/time.Duration(rl.perMinute)), rl.perMinute)}
		rl.clients[ip] = c
	}
	c.seen = now
	return c.lim.AllowN(now, 1)
}

// sweepLocked drops clients idle for 2+ minutes (their bucket is full again).
func (rl *rateLimiter) sweepLocked(now time.Time) {
	for k, c := range rl.clients {
		if now.Sub(c.seen) > 2*time.Minute {
			delete(rl.clients, k)
		}
	}
}

func (rl *rateLimiter) evictOldestLocked() {
	var oldest string
	var at time.Time
	first := true
	for k, c := range rl.clients {
		if first || c.seen.Before(at) {
			oldest, at, first = k, c.seen, false
		}
	}
	delete(rl.clients, oldest)
}

func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(clientIP(r, rl.trustProxy)) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, CodeRateLimited, "too many requests; try again in a minute", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) clientIP(r *http.Request) string { return clientIP(r, s.proxy) }

// clientIP is the host part of r.RemoteAddr. Only when trustProxy is set
// (TRUST_PROXY=1, e.g. behind `tailscale serve`) AND the direct peer is
// loopback is X-Forwarded-For used: then the right-most valid entry, i.e.
// the address that proxy itself saw. A remote client cannot spoof its key
// because its own RemoteAddr is never loopback; without TRUST_PROXY a local
// process cannot pick its bucket either.
func clientIP(r *http.Request, trustProxy bool) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !trustProxy {
		return host
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
