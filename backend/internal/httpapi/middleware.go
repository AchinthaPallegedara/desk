package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/claviq/desk/backend/internal/domain"
	"github.com/claviq/desk/backend/internal/models"
	"github.com/claviq/desk/backend/internal/reqctx"
)

type middleware func(http.Handler) http.Handler

func chain(h http.Handler, mws ...middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 64 {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(reqctx.WithRequestID(r.Context(), id)))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush keeps streaming (MCP SSE) working through the wrapper.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func requestLogger(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			path := r.URL.Path
			if strings.HasPrefix(path, "/api/public/p/") {
				path = "/api/public/p/…" // don't log bearer-like link tokens
			}
			log.Info("http", "method", r.Method, "path", path, "status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(), "request_id", reqctx.RequestID(r.Context()))
		})
	}
}

func recoverer(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					log.Error("panic", "error", v, "request_id", reqctx.RequestID(r.Context()))
					writeError(w, http.StatusInternalServerError, "internal", "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// cors allows browser-based MCP clients/inspectors. Only used on endpoints that
// authenticate with bearer tokens or are public, never with cookies.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID")
		h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		h.Set("Access-Control-Expose-Headers", "Mcp-Session-Id, WWW-Authenticate")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- session auth for /api ----------

type actorKey struct{}

func actorFrom(ctx context.Context) domain.Actor { return ctx.Value(actorKey{}).(domain.Actor) }

type userKey struct{}

func userFrom(ctx context.Context) *models.User { return ctx.Value(userKey{}).(*models.User) }

// authed requires a valid owner session, checks Origin on writes (CSRF
// defence on top of SameSite=Lax), rate-limits per user and builds the Actor.
func (a *api) authed(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.Sessions.FromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "please sign in")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !a.sameOrigin(r) {
			writeError(w, http.StatusForbidden, "forbidden", "cross-origin request rejected")
			return
		}
		if !a.limits.allow("api:"+u.ID.String(), 600, time.Minute) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests; slow down")
			return
		}
		uid := u.ID
		actor := domain.Actor{TenantID: u.TenantID, UserID: &uid, Via: "web", RequestID: reqctx.RequestID(r.Context())}
		ctx := context.WithValue(r.Context(), actorKey{}, actor)
		ctx = context.WithValue(ctx, userKey{}, u)
		h(w, r.WithContext(ctx))
	})
}

func (a *api) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser client or same-origin navigation without Origin
	}
	o, err := url.Parse(origin)
	p, err2 := url.Parse(a.PublicURL)
	if err != nil || err2 != nil {
		return false
	}
	return o.Scheme == p.Scheme && o.Host == p.Host
}

// ---------- rate limiting ----------

type limiters struct {
	mu sync.Mutex
	m  map[string]*limiterEntry
}

type limiterEntry struct {
	l    *rate.Limiter
	seen time.Time
}

func newLimiters() *limiters {
	l := &limiters{m: map[string]*limiterEntry{}}
	go func() {
		for range time.Tick(10 * time.Minute) {
			l.mu.Lock()
			for k, e := range l.m {
				if time.Since(e.seen) > time.Hour {
					delete(l.m, k)
				}
			}
			l.mu.Unlock()
		}
	}()
	return l
}

// allow applies a token bucket of n events per window, burst n.
func (l *limiters) allow(key string, n int, per time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.m[key]
	if !ok {
		e = &limiterEntry{l: rate.NewLimiter(rate.Every(per/time.Duration(n)), n)}
		l.m[key] = e
	}
	e.seen = time.Now()
	return e.l.Allow()
}

func (a *api) limit(name string, n int, per time.Duration, h http.HandlerFunc) http.Handler {
	return a.limitHandler(name, n, per, h)
}

func (a *api) limitHandler(name string, n int, per time.Duration, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.limits.allow(name+":"+clientIP(r), n, per) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests; try again shortly")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// clientIP returns the real client address. Requests reach the service through
// reverse proxies (Nginx Proxy Manager -> Next.js -> Go in Docker), all on
// loopback or private networks. X-Forwarded-For is read right to left, skipping
// those trusted hops, so a client cannot spoof its address by sending its own
// header: the first untrusted entry is the one the outermost proxy appended.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !trustedProxy(net.ParseIP(host)) {
		return host
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		ip := net.ParseIP(p)
		if ip == nil {
			continue
		}
		if !trustedProxy(ip) {
			return p
		}
		host = p
	}
	return host
}

func trustedProxy(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	for _, n := range cloudflareNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// cloudflareNets are Cloudflare's proxy ranges (https://www.cloudflare.com/ips/).
// desk.claviq.com is proxied by Cloudflare, so its edge servers are a trusted hop
// in X-Forwarded-For, not the client.
var cloudflareNets = func() []*net.IPNet {
	cidrs := []string{
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
		"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
		"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
		"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
		"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}()
