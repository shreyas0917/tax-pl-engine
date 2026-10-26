// Package app wires configuration, logging, database, blob storage, Redis and the jobs
// service for both binaries (api and worker).
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/shreyas0917/taxpl/internal/blob"
	"github.com/shreyas0917/taxpl/internal/config"
	"github.com/shreyas0917/taxpl/internal/jobs"
	"github.com/shreyas0917/taxpl/internal/pipeline"
	"github.com/shreyas0917/taxpl/internal/report/jsonreport"
	"github.com/shreyas0917/taxpl/internal/report/pdfreport"
	"github.com/shreyas0917/taxpl/internal/report/xlsxreport"
	"github.com/shreyas0917/taxpl/internal/store"
)

// App is the shared runtime.
type App struct {
	Cfg   config.Config
	Log   *slog.Logger
	Pool  *pgxpool.Pool
	Blob  blob.Store
	Redis *redis.Client
	Jobs  *jobs.Service
}

// Logger builds the slog logger: text in dev, JSON otherwise.
func Logger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if cfg.IsDev() {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

// New loads everything. Redis is optional: when REDIS_URL is empty or unreachable the API
// falls back to in-memory rate limiting and idempotency.
func New(ctx context.Context) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	log := Logger(cfg)
	slog.SetDefault(log)

	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	var bs blob.Store
	switch cfg.BlobDriver {
	case "s3":
		bs, err = blob.NewS3(ctx, blob.S3Options{Endpoint: cfg.S3Endpoint, AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey, Bucket: cfg.S3BucketRaw})
	default:
		bs, err = blob.NewLocal(cfg.BlobLocalDir)
	}
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("blob store: %w", err)
	}
	var rdb *redis.Client
	if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			log.Warn("REDIS_URL invalid; using in-memory fallbacks", "err", err)
		} else {
			rdb = redis.NewClient(opt)
			if err := rdb.Ping(ctx).Err(); err != nil {
				log.Warn("redis unreachable; using in-memory fallbacks", "err", err)
				_ = rdb.Close()
				rdb = nil
			}
		}
	}
	js := &jobs.Service{Pool: pool, Blob: bs, Log: log, RawTTL: cfg.RawFileTTL, ReportWriters: ReportWriters()}
	return &App{Cfg: cfg, Log: log, Pool: pool, Blob: bs, Redis: rdb, Jobs: js}, nil
}

// ReportWriters lists the available report formats.
func ReportWriters() map[string]jobs.ReportWriter {
	return map[string]jobs.ReportWriter{
		"xlsx": xlsxreport.Write,
		"pdf":  pdfreport.Write,
		"json": func(rep *pipeline.Report, _ string) ([]byte, string, error) {
			b, err := jsonreport.Marshal(rep)
			return b, "application/json", err
		},
	}
}

// Close releases resources.
func (a *App) Close() {
	if a.Redis != nil {
		_ = a.Redis.Close()
	}
	a.Pool.Close()
}
