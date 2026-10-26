package store

import (
	"context"
	"fmt"
	"time"

	pgxdecimal "github.com/jackc/pgx-shopspring-decimal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolOptions tune NewPool.
type PoolOptions struct {
	// AppRole, when set, is assumed with SET ROLE on every connection so that row-level
	// security policies apply (the login role is typically the table owner or a superuser,
	// which would bypass them). Empty disables the switch.
	AppRole  string
	MaxConns int32
}

// NewPool opens a pgx pool with shopspring/decimal registered for NUMERIC columns.
func NewPool(ctx context.Context, url string, opts ...PoolOptions) (*pgxpool.Pool, error) {
	var o PoolOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 16
	if o.MaxConns > 0 {
		cfg.MaxConns = o.MaxConns
	}
	cfg.MaxConnLifetime = time.Hour
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		pgxdecimal.Register(conn.TypeMap())
		if o.AppRole != "" {
			if _, err := conn.Exec(ctx, "SET ROLE "+pgx.Identifier{o.AppRole}.Sanitize()); err != nil {
				return fmt.Errorf("set role %s: %w", o.AppRole, err)
			}
		}
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// DB is the subset of pgxpool.Pool and pgxpool.Conn used to start transactions.
type DB interface {
	DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// WithTx runs fn inside a transaction, committing on nil error.
func WithTx(ctx context.Context, db DB, fn func(tx pgx.Tx, q *Queries) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx, New(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// WithUserTx is WithTx with the row-level-security context set for the transaction.
func WithUserTx(ctx context.Context, db DB, userID string, fn func(tx pgx.Tx, q *Queries) error) error {
	return WithTx(ctx, db, func(tx pgx.Tx, q *Queries) error {
		if err := SetUser(ctx, tx, userID, true); err != nil {
			return err
		}
		return fn(tx, q)
	})
}

// SetUser sets app.user_id on the transaction (local) or the session.
func SetUser(ctx context.Context, exec interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}, userID string, local bool) error {
	scope := "false"
	if local {
		scope = "true"
	}
	_, err := exec.Exec(ctx, "SELECT set_config('app.user_id', $1, "+scope+")", userID)
	return err
}

// AcquireUser checks out a connection bound to one user (session-level app.user_id).
// The caller must call the returned release function, which clears the setting first.
func AcquireUser(ctx context.Context, pool *pgxpool.Pool, userID string) (*pgxpool.Conn, func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := SetUser(ctx, conn, userID, false); err != nil {
		conn.Release()
		return nil, nil, err
	}
	release := func() {
		_, _ = conn.Exec(context.Background(), "SELECT set_config('app.user_id', '', false)")
		conn.Release()
	}
	return conn, release, nil
}

// LockUser serialises all recomputation for one user inside the current transaction
// (spec Section 8): concurrent uploads queue up instead of corrupting derived tables.
func LockUser(ctx context.Context, tx pgx.Tx, userID string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))", userID)
	return err
}
