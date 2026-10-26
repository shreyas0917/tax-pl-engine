package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/shreyas0917/taxpl/internal/auth"
	"github.com/shreyas0917/taxpl/internal/blob"
	"github.com/shreyas0917/taxpl/internal/config"
	"github.com/shreyas0917/taxpl/internal/jobs"
	"github.com/shreyas0917/taxpl/internal/store"
)

// Server holds the API dependencies.
type Server struct {
	cfg    config.Config
	log    *slog.Logger
	pool   *pgxpool.Pool // user-facing pool (RLS role)
	sysDB  *pgxpool.Pool // authentication lookups and health checks
	sys    *store.Queries
	blob   blob.Store
	jobs   *jobs.Service
	tokens *auth.Tokens
	rdb    *redis.Client

	idemMu    sync.Mutex
	idemLocal map[string]idemRecord
	now       func() time.Time
}

// New builds the server. rdb may be nil (rate limits and idempotency fall back to memory).
func New(cfg config.Config, log *slog.Logger, pool, sysPool *pgxpool.Pool, bs blob.Store, js *jobs.Service, rdb *redis.Client) *Server {
	if sysPool == nil {
		sysPool = pool
	}
	return &Server{cfg: cfg, log: log, pool: pool, sysDB: sysPool, sys: store.New(sysPool), blob: bs, jobs: js, rdb: rdb,
		tokens: auth.NewTokens(cfg.JWTSecret, cfg.AccessTokenTTL), idemLocal: map[string]idemRecord{}, now: time.Now}
}

// Router assembles the chi router (spec Section 9).
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(s.log))
	r.Use(middleware.Recoverer)
	r.Use(cors(s.cfg.FrontendOrigin))
	r.Use(middleware.Timeout(120 * time.Second))

	ipLimiter := newLimiter(s.rdb, 120, time.Minute)
	authLimiter := newLimiter(s.rdb, 20, time.Minute)
	userLimiter := newLimiter(s.rdb, 300, time.Minute)

	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.rateLimit(byIP, ipLimiter))
		r.Get("/rules", s.listRules)
		r.Get("/brokers", s.listBrokers)

		r.Group(func(r chi.Router) {
			r.Use(s.rateLimit(byIP, authLimiter))
			r.Post("/auth/register", s.register)
			r.Post("/auth/login", s.login)
			r.Post("/auth/refresh", s.refresh)
			r.Post("/auth/logout", s.logout)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.rateLimit(byUser, userLimiter))
			r.Use(s.idempotent)

			r.Get("/me", s.me)
			r.Delete("/me", s.deleteMe)
			r.Post("/me/password", s.changePassword)
			r.Post("/me/pan", s.setPAN)

			r.Get("/accounts", s.listAccounts)
			r.Post("/accounts", s.createAccount)
			r.Delete("/accounts/{id}", s.deleteAccount)

			r.Post("/uploads", s.upload)
			r.Get("/uploads", s.listUploads)

			r.Get("/jobs", s.listJobs)
			r.Get("/jobs/{id}", s.getJob)
			r.Get("/jobs/{id}/events", s.jobEvents)

			r.Get("/review-items", s.listReviewItems)
			r.Post("/review-items/{id}/resolve", s.resolveReviewItem)

			r.Get("/summary", s.summary)
			r.Get("/gains", s.listGains)
			r.Get("/gains/{id}/explain", s.explainGain)
			r.Get("/trades", s.listTrades)
			r.Get("/holdings", s.listHoldings)
			r.Post("/holdings/reconcile", s.reconcileHoldings)

			r.Post("/trades/manual", s.addManualTrade)

			r.Post("/reports", s.createReport)
			r.Get("/reports", s.listReports)
			r.Get("/reports/{id}/download", s.downloadReport)
		})
	})
	return r
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	out := map[string]string{"database": "ok", "redis": "not configured"}
	status := 200
	if err := s.sysDB.Ping(ctx); err != nil {
		out["database"] = "unreachable"
		status = 503
	}
	if s.rdb != nil {
		if err := s.rdb.Ping(ctx).Err(); err != nil {
			out["redis"] = "unreachable"
			status = 503
		} else {
			out["redis"] = "ok"
		}
	}
	writeJSON(w, status, out)
}
