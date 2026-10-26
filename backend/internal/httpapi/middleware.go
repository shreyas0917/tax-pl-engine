package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shreyas0917/taxpl/internal/store"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
)

type ctxKey int

const (
	userKey ctxKey = 1
	connKey ctxKey = 2
)

// userConn is the per-request connection with app.user_id set (row-level security).
type userConn struct {
	conn    *pgxpool.Conn
	release func()
	once    sync.Once
}

func (u *userConn) done() { u.once.Do(u.release) }

// qry returns queries bound to the request user, or the system queries for public routes.
func (s *Server) qry(r *http.Request) *store.Queries {
	if uc, ok := r.Context().Value(connKey).(*userConn); ok {
		return store.New(uc.conn)
	}
	return s.sys
}

// db returns the transaction starter bound to the request user.
func (s *Server) db(r *http.Request) store.DB {
	if uc, ok := r.Context().Value(connKey).(*userConn); ok {
		return uc.conn
	}
	return s.sysDB
}

// releaseUserConn hands the connection back early (long-lived streams).
func releaseUserConn(r *http.Request) {
	if uc, ok := r.Context().Value(connKey).(*userConn); ok {
		uc.done()
	}
}

// UserID returns the authenticated user ID from the context.
func UserID(ctx context.Context) string {
	v, _ := ctx.Value(userKey).(string)
	return v
}

// requireAuth validates the Bearer access token.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token := ""
		if strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(h[7:])
		} else if t := r.URL.Query().Get("access_token"); t != "" && strings.HasSuffix(r.URL.Path, "/events") {
			token = t // EventSource cannot set headers
		}
		if token == "" {
			writeProblem(w, r, unauthorized("missing bearer token"))
			return
		}
		uid, err := s.tokens.Verify(token)
		if err != nil {
			writeProblem(w, r, unauthorized("invalid or expired token"))
			return
		}
		conn, release, err := store.AcquireUser(r.Context(), s.pool, uid)
		if err != nil {
			writeProblem(w, r, err)
			return
		}
		uc := &userConn{conn: conn, release: release}
		defer uc.done()
		ctx := context.WithValue(r.Context(), userKey, uid)
		ctx = context.WithValue(ctx, connKey, uc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// cors allows only the configured frontend origin.
func cors(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if o := r.Header.Get("Origin"); o != "" && o == origin {
				w.Header().Set("Access-Control-Allow-Origin", o)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, Last-Event-ID")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")
				w.Header().Set("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requestLogger emits one structured line per request with the request ID and user ID.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			if strings.HasSuffix(r.URL.Path, "/healthz") {
				return
			}
			log.Info("request", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()), "user_id", UserID(r.Context()))
		})
	}
}

// --- rate limiting ----------------------------------------------------------------------

// limiter is a fixed-window counter backed by Redis when available, else in memory.
type limiter struct {
	rdb    *redis.Client
	mu     sync.Mutex
	local  map[string]*window
	limit  int
	period time.Duration
}

type window struct {
	count int
	reset time.Time
}

func newLimiter(rdb *redis.Client, limit int, period time.Duration) *limiter {
	return &limiter{rdb: rdb, local: map[string]*window{}, limit: limit, period: period}
}

func (l *limiter) allow(ctx context.Context, key string) bool {
	if l.rdb != nil {
		k := "rl:" + key + ":" + time.Now().Truncate(l.period).Format("150405")
		n, err := l.rdb.Incr(ctx, k).Result()
		if err == nil {
			if n == 1 {
				l.rdb.Expire(ctx, k, l.period)
			}
			return n <= int64(l.limit)
		}
		// Redis down: fall through to the local limiter rather than failing open silently.
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	wdw := l.local[key]
	if wdw == nil || now.After(wdw.reset) {
		wdw = &window{reset: now.Add(l.period)}
		l.local[key] = wdw
	}
	wdw.count++
	if len(l.local) > 10000 {
		for k, v := range l.local {
			if now.After(v.reset) {
				delete(l.local, k)
			}
		}
	}
	return wdw.count <= l.limit
}

func (s *Server) rateLimit(keyFn func(r *http.Request) string, lim *limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !lim.allow(r.Context(), keyFn(r)) {
				w.Header().Set("Retry-After", "60")
				writeProblem(w, r, &APIError{Status: 429, Title: "Too many requests", Detail: "Slow down and try again in a minute."})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func byIP(r *http.Request) string   { return "ip:" + clientIP(r) }
func byUser(r *http.Request) string { return "user:" + UserID(r.Context()) }

// --- idempotency keys --------------------------------------------------------------------

type idemRecord struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

// idempotent replays a stored response for a repeated Idempotency-Key within 24 hours.
func (s *Server) idempotent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		if len(key) > 200 {
			writeProblem(w, r, badRequest("Idempotency-Key too long"))
			return
		}
		full := UserID(r.Context()) + ":" + r.URL.Path + ":" + key
		if rec, ok := s.idemGet(r.Context(), full); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(rec.Status)
			_, _ = w.Write(rec.Body)
			return
		}
		rec := &recorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		if rec.status < 300 {
			s.idemPut(r.Context(), full, idemRecord{Status: rec.status, Body: rec.buf})
		}
	})
}

type recorder struct {
	http.ResponseWriter
	status int
	buf    []byte
}

func (r *recorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }
func (r *recorder) Write(b []byte) (int, error) {
	r.buf = append(r.buf, b...)
	return r.ResponseWriter.Write(b)
}

func (s *Server) idemGet(ctx context.Context, key string) (idemRecord, bool) {
	var rec idemRecord
	if s.rdb != nil {
		b, err := s.rdb.Get(ctx, "idem:"+key).Bytes()
		if err == nil && json.Unmarshal(b, &rec) == nil {
			return rec, true
		}
		if err == nil || errors.Is(err, redis.Nil) {
			return rec, false
		}
	}
	s.idemMu.Lock()
	defer s.idemMu.Unlock()
	rec, ok := s.idemLocal[key]
	return rec, ok
}

func (s *Server) idemPut(ctx context.Context, key string, rec idemRecord) {
	if s.rdb != nil {
		b, _ := json.Marshal(rec)
		if err := s.rdb.Set(ctx, "idem:"+key, b, 24*time.Hour).Err(); err == nil {
			return
		}
	}
	s.idemMu.Lock()
	defer s.idemMu.Unlock()
	s.idemLocal[key] = rec
}
