package jobs_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/shreyas0917/taxpl/internal/blob"
	"github.com/shreyas0917/taxpl/internal/jobs"
	_ "github.com/shreyas0917/taxpl/internal/parser/all"
	"github.com/shreyas0917/taxpl/internal/pipeline"
	"github.com/shreyas0917/taxpl/internal/store"
)

// These tests need Docker. Run with: TAXPL_INTEGRATION=1 go test ./internal/jobs/ -run Integration -v

type env struct {
	pool *pgxpool.Pool // app role, RLS enforced
	sys  *pgxpool.Pool // superuser, bypasses RLS
	q    *store.Queries
	svc  *jobs.Service
	dir  string
}

func setup(t *testing.T) *env {
	t.Helper()
	if os.Getenv("TAXPL_INTEGRATION") == "" {
		t.Skip("set TAXPL_INTEGRATION=1 to run Docker-backed tests")
	}
	ctx := context.Background()
	url := os.Getenv("TAXPL_TEST_DATABASE_URL")
	if url == "" {
		// Docker Desktop on Windows often cannot run the Ryuk reaper; containers are terminated by t.Cleanup instead.
		if os.Getenv("TESTCONTAINERS_RYUK_DISABLED") == "" {
			t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
		}
		pg, err := tcpostgres.Run(ctx, "postgres:16-alpine", tcpostgres.WithDatabase("taxpl"), tcpostgres.WithUsername("taxpl"), tcpostgres.WithPassword("taxpl"),
			testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(6*time.Minute)))
		require.NoError(t, err)
		t.Cleanup(func() { _ = pg.Terminate(ctx) })
		url, err = pg.ConnectionString(ctx, "sslmode=disable")
		require.NoError(t, err)
	}

	sqlDB, err := sql.Open("pgx", url)
	require.NoError(t, err)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.Up(sqlDB, filepath.Join("..", "..", "db", "migrations")))
	require.NoError(t, sqlDB.Close())

	sys, err := store.NewPool(ctx, url)
	require.NoError(t, err)
	t.Cleanup(sys.Close)
	pool, err := store.NewPool(ctx, url, store.PoolOptions{AppRole: "taxpl_app"})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, jobs.Migrate(ctx, sys))
	jobs.Queue = "integration-test" // never served by a worker
	client, err := jobs.NewInsertOnlyClient(pool)
	require.NoError(t, err)
	dir := t.TempDir()
	bs, err := blob.NewLocal(dir)
	require.NoError(t, err)
	svc := &jobs.Service{Pool: pool, Sys: sys, Blob: bs, Log: slog.New(slog.NewTextHandler(os.Stderr, nil)), River: client, RawTTL: 24 * time.Hour}
	return &env{pool: pool, sys: sys, q: store.New(sys), svc: svc, dir: dir}
}

var _ = stdlib.GetDefaultDriver

// user creates a test user with a per-run unique email and removes it (and everything it
// owns, by cascade) when the test ends, so the tests can share a long-lived database.
func (e *env) user(t *testing.T, email string) string {
	email = fmt.Sprintf("%d-%s", time.Now().UnixNano(), email)
	u, err := e.q.CreateUser(context.Background(), store.CreateUserParams{Email: email, PasswordHash: "x"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.q.DeleteUser(context.Background(), u.ID) })
	return u.ID
}

func (e *env) account(t *testing.T, uid, nick string) string {
	a, err := e.q.CreateAccount(context.Background(), store.CreateAccountParams{UserID: uid, Broker: "zerodha", Nickname: nick})
	require.NoError(t, err)
	return a.ID
}

// upload mimics the API handler: blob + uploads row + job row. Returns ("", "") for duplicates.
func (e *env) upload(t *testing.T, uid, acct, name string, data []byte) (uploadID, jobID string) {
	ctx := context.Background()
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	key := fmt.Sprintf("raw/%s/%s.csv", uid, sha[:16])
	require.NoError(t, e.svc.Blob.Put(ctx, key, bytes.NewReader(data), "text/csv"))
	up, err := e.q.CreateUpload(ctx, store.CreateUploadParams{UserID: uid, AccountID: acct, Broker: "zerodha", Filename: name, Sha256: sha, SizeBytes: int64(len(data)), BlobKey: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ""
	}
	require.NoError(t, err)
	job, err := e.q.CreateJob(ctx, store.CreateJobParams{UserID: uid, UploadID: &up.ID, Kind: jobs.KindProcessUpload, State: jobs.StateUploaded, Payload: []byte("{}")})
	require.NoError(t, err)
	return up.ID, job.ID
}

func (e *env) process(t *testing.T, uid, upID, jobID string) {
	require.NoError(t, e.svc.ProcessUpload(context.Background(), jobs.ProcessUploadArgs{UserID: uid, UploadID: upID, JobID: jobID}))
}

func (e *env) recompute(t *testing.T, uid string) store.Computation {
	require.NoError(t, e.svc.Recompute(context.Background(), uid, ""))
	c, err := e.q.LatestComputation(context.Background(), uid)
	require.NoError(t, err)
	return c
}

const hdr = "symbol,isin,trade_date,exchange,segment,series,trade_type,auction,quantity,price,trade_id,order_id,order_execution_time\n"

func csvA() []byte {
	return []byte(hdr + "INFY,INE009A01021,2024-06-03,NSE,EQ,EQ,buy,false,10,1400,1,10,2024-06-03T10:00:00\n" +
		"INFY,INE009A01021,2025-02-10,NSE,EQ,EQ,sell,false,10,1500,2,20,2025-02-10T10:00:00\n" +
		"TCS,INE467B01029,2024-07-01,NSE,EQ,EQ,buy,false,5,4000,3,30,2024-07-01T10:00:00\n")
}

func csvB() []byte {
	return []byte(hdr + "TCS,INE467B01029,2025-05-05,NSE,EQ,EQ,sell,false,5,4200,4,40,2025-05-05T10:00:00\n" +
		"SBIN,INE062A01020,2025-05-06,NSE,EQ,EQ,buy,false,100,800,5,50,2025-05-06T10:00:00\n" +
		"SBIN,INE062A01020,2025-05-06,NSE,EQ,EQ,sell,false,100,810,6,60,2025-05-06T14:00:00\n")
}

func csvC() []byte {
	return []byte(hdr + "RELIANCE,INE002A01018,2025-06-01,NSE,EQ,EQ,buy,false,20,2500,7,70,2025-06-01T10:00:00\n" +
		"RELIANCE,INE002A01018,2025-09-01,NSE,EQ,EQ,sell,false,8,2700,8,80,2025-09-01T10:00:00\n")
}

func TestIntegrationIdempotentUpload(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	uid := e.user(t, "idem@example.com")
	acct := e.account(t, uid, "Main")

	upID, jobID := e.upload(t, uid, acct, "a.csv", csvA())
	e.process(t, uid, upID, jobID)
	c1 := e.recompute(t, uid)
	n1, _ := e.q.CountTrades(ctx, uid)
	require.EqualValues(t, 3, n1)

	// Same bytes again: no new upload row, re-processing the existing one adds nothing.
	dupUp, dupJob := e.upload(t, uid, acct, "a-again.csv", csvA())
	require.Empty(t, dupUp)
	require.Empty(t, dupJob)
	e.process(t, uid, upID, jobID)
	n2, _ := e.q.CountTrades(ctx, uid)
	require.EqualValues(t, 3, n2, "re-processing must not double-count")
	c2 := e.recompute(t, uid)
	require.Equal(t, c1.Checksum, c2.Checksum, "identical inputs give an identical computation")

	gains, err := e.q.ListGainsForFY(ctx, store.ListGainsForFYParams{UserID: uid, Fy: "2024-25"})
	require.NoError(t, err)
	require.Len(t, gains, 1)
	require.Equal(t, "1000", gains[0].Gain.String())
	require.Equal(t, "STCG_EQUITY", gains[0].Category)

	job, err := e.q.GetJobAny(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, jobs.StateReportReady, job.State)
	events, err := e.q.ListJobEvents(ctx, store.ListJobEventsParams{JobID: jobID, ID: 0})
	require.NoError(t, err)
	var states []string
	for _, ev := range events {
		states = append(states, ev.State)
	}
	require.Contains(t, states, jobs.StateParsing)
	require.Contains(t, states, jobs.StateNormalized)
	require.Contains(t, states, jobs.StateMatched)
	require.Contains(t, states, jobs.StateComputed)
}

// TestIntegrationInterruptedRecompute simulates a worker dying mid-job: the transaction is
// aborted, nothing partial is visible, and the retry completes with the same result.
func TestIntegrationInterruptedRecompute(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	uid := e.user(t, "crash@example.com")
	acct := e.account(t, uid, "Main")
	upID, jobID := e.upload(t, uid, acct, "a.csv", csvA())
	e.process(t, uid, upID, jobID)
	c1 := e.recompute(t, uid)

	upID2, jobID2 := e.upload(t, uid, acct, "b.csv", csvB())
	e.process(t, uid, upID2, jobID2)

	// Kill: cancel the context immediately so the recompute transaction can never commit.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	err := e.svc.Recompute(cctx, uid, "")
	require.Error(t, err)
	latest, err := e.q.LatestComputation(ctx, uid)
	require.NoError(t, err)
	require.Equal(t, c1.ID, latest.ID, "an interrupted recompute leaves the previous computation intact")
	job, _ := e.q.GetJobAny(ctx, jobID2)
	require.Equal(t, jobs.StateNormalized, job.State, "the job is still waiting, not half-advanced")

	// Restart: the retry succeeds and includes the second file.
	c2 := e.recompute(t, uid)
	require.NotEqual(t, c1.Checksum, c2.Checksum)
	require.EqualValues(t, 6, c2.TradeCount)
	job, _ = e.q.GetJobAny(ctx, jobID2)
	require.Equal(t, jobs.StateReportReady, job.State)
	c3 := e.recompute(t, uid)
	require.Equal(t, c2.Checksum, c3.Checksum, "replay is stable")
}

// TestIntegrationConcurrentUploads: three files processed concurrently (with concurrent
// recomputes racing on the advisory lock) must equal three files processed one by one.
func TestIntegrationConcurrentUploads(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	files := map[string][]byte{"a.csv": csvA(), "b.csv": csvB(), "c.csv": csvC()}

	seq := e.user(t, "seq@example.com")
	seqAcct := e.account(t, seq, "Main")
	for _, name := range []string{"a.csv", "b.csv", "c.csv"} {
		up, job := e.upload(t, seq, seqAcct, name, files[name])
		e.process(t, seq, up, job)
		e.recompute(t, seq)
	}

	par := e.user(t, "par@example.com")
	parAcct := e.account(t, par, "Main")
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for name, data := range files {
		wg.Add(1)
		go func(name string, data []byte) {
			defer wg.Done()
			sum := sha256.Sum256(data)
			sha := hex.EncodeToString(sum[:])
			key := fmt.Sprintf("raw/%s/%s.csv", par, sha[:16])
			if err := e.svc.Blob.Put(ctx, key, bytes.NewReader(data), "text/csv"); err != nil {
				errs <- err
				return
			}
			up, err := e.q.CreateUpload(ctx, store.CreateUploadParams{UserID: par, AccountID: parAcct, Broker: "zerodha", Filename: name, Sha256: sha, SizeBytes: int64(len(data)), BlobKey: key})
			if err != nil {
				errs <- err
				return
			}
			job, err := e.q.CreateJob(ctx, store.CreateJobParams{UserID: par, UploadID: &up.ID, Kind: jobs.KindProcessUpload, State: jobs.StateUploaded, Payload: []byte("{}")})
			if err != nil {
				errs <- err
				return
			}
			if err := e.svc.ProcessUpload(ctx, jobs.ProcessUploadArgs{UserID: par, UploadID: up.ID, JobID: job.ID}); err != nil {
				errs <- err
				return
			}
			errs <- e.svc.Recompute(ctx, par, "")
		}(name, data)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	e.recompute(t, par) // final pass, as the queue would run after the last upload

	require.Equal(t, canonical(t, e, seq), canonical(t, e, par), "concurrent processing must equal sequential processing")
	waiting, _ := e.q.ListJobsInState(ctx, store.ListJobsInStateParams{UserID: par, State: jobs.StateNormalized})
	require.Empty(t, waiting, "no job left behind")
}

// canonical projects a user's derived data onto content only (no IDs, account nicknames
// instead of UUIDs) so two users with the same trades compare equal.
func canonical(t *testing.T, e *env, uid string) string {
	t.Helper()
	ctx := context.Background()
	req, err := jobs.BuildRequest(ctx, e.q, uid)
	require.NoError(t, err)
	accts, _ := e.q.ListAccounts(ctx, uid)
	names := map[string]string{}
	for _, a := range accts {
		names[a.ID] = a.Nickname
	}
	rep, err := pipeline.Run(req)
	require.NoError(t, err)
	var lines []string
	for _, g := range rep.Gains {
		lines = append(lines, fmt.Sprintf("gain|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s", g.FY, g.Category, names[g.AccountID], g.Symbol, g.SaleDate, g.AcquisitionDate,
			g.Quantity.StringFixed(6), g.SaleValue.StringFixed(4), g.CostOfAcquisition.StringFixed(4), g.Gain.StringFixed(4)))
	}
	for _, l := range rep.Lots {
		lines = append(lines, fmt.Sprintf("lot|%s|%s|%s|%s|%s", names[l.AccountID], l.Symbol, l.AcquisitionDate, l.QuantityOpen.StringFixed(6), l.CostOpen.StringFixed(4)))
	}
	for _, r := range rep.Review {
		lines = append(lines, fmt.Sprintf("review|%s|%s|%s", r.Kind, r.Symbol, r.Payload["quantity"]))
	}
	sort.Strings(lines)
	// The stored gains must agree with the fresh run.
	stored, err := e.q.ListGainsForFY(ctx, store.ListGainsForFYParams{UserID: uid, Fy: "2025-26"})
	require.NoError(t, err)
	fresh := 0
	for _, g := range rep.Gains {
		if g.FY == "2025-26" {
			fresh++
		}
	}
	require.Equal(t, fresh, len(stored))
	return strings.Join(lines, "\n")
}

func TestIntegrationPurgeAndDelete(t *testing.T) {
	if os.Getenv("TAXPL_TEST_DATABASE_URL") != "" {
		t.Skip("purge is global; run this one against a throwaway container")
	}
	e := setup(t)
	ctx := context.Background()
	uid := e.user(t, "purge@example.com")
	acct := e.account(t, uid, "Main")
	upID, jobID := e.upload(t, uid, acct, "a.csv", csvA())
	e.process(t, uid, upID, jobID)
	e.recompute(t, uid)

	up, _ := e.q.GetUpload(ctx, store.GetUploadParams{ID: upID, UserID: uid})
	exists, _ := e.svc.Blob.Exists(ctx, up.BlobKey)
	require.True(t, exists)
	// Nothing to purge yet.
	require.NoError(t, e.svc.PurgeRawFiles(ctx, time.Now()))
	exists, _ = e.svc.Blob.Exists(ctx, up.BlobKey)
	require.True(t, exists)
	// After the TTL the raw file goes, the trades stay.
	require.NoError(t, e.svc.PurgeRawFiles(ctx, time.Now().Add(48*time.Hour)))
	exists, _ = e.svc.Blob.Exists(ctx, up.BlobKey)
	require.False(t, exists, "raw file purged")
	up, _ = e.q.GetUpload(ctx, store.GetUploadParams{ID: upID, UserID: uid})
	require.NotNil(t, up.PurgedAt)
	n, _ := e.q.CountTrades(ctx, uid)
	require.EqualValues(t, 3, n, "parsed trades are kept")

	// Delete my data removes every row.
	require.NoError(t, e.svc.DeleteUserData(ctx, uid))
	_, err := e.q.GetUser(ctx, uid)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	n, _ = e.q.CountTrades(ctx, uid)
	require.EqualValues(t, 0, n)
	var remaining int
	require.NoError(t, e.sys.QueryRow(ctx, "SELECT count(*) FROM gains WHERE user_id = $1", uid).Scan(&remaining))
	require.Equal(t, 0, remaining)
	var audits int
	require.NoError(t, e.sys.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE user_id = $1 AND action = 'delete_all_data'", uid).Scan(&audits))
	require.Equal(t, 1, audits, "the deletion itself is audited")
	_ = json.Marshal
}

// TestIntegrationRowLevelSecurity: with app.user_id set to A, user B's rows are invisible
// and cannot be written, even though the role has table-level privileges.
func TestIntegrationRowLevelSecurity(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	a := e.user(t, "rls-a@example.com")
	b := e.user(t, "rls-b@example.com")
	acctA := e.account(t, a, "Main")
	acctB := e.account(t, b, "Main")
	upA, jobA := e.upload(t, a, acctA, "a.csv", csvA())
	e.process(t, a, upA, jobA)
	upB, jobB := e.upload(t, b, acctB, "b.csv", csvB())
	e.process(t, b, upB, jobB)
	e.recompute(t, a)
	e.recompute(t, b)

	var seen int64
	require.NoError(t, store.WithUserTx(ctx, e.pool, a, func(_ pgx.Tx, q *store.Queries) error {
		var err error
		seen, err = q.CountTrades(ctx, b)
		return err
	}))
	require.EqualValues(t, 0, seen, "A cannot count B's trades")
	require.NoError(t, store.WithUserTx(ctx, e.pool, a, func(_ pgx.Tx, q *store.Queries) error {
		rows, err := q.ListTradesForUser(ctx, b)
		require.Empty(t, rows)
		return err
	}))
	// Without any user context nothing is visible at all.
	require.NoError(t, store.WithTx(ctx, e.pool, func(_ pgx.Tx, q *store.Queries) error {
		rows, err := q.ListTradesForUser(ctx, a)
		require.Empty(t, rows)
		return err
	}))
	// Writing into another user's space is rejected by the policy.
	err := store.WithUserTx(ctx, e.pool, a, func(_ pgx.Tx, q *store.Queries) error {
		_, err := q.CreateAccount(ctx, store.CreateAccountParams{UserID: b, Broker: "zerodha", Nickname: "Injected"})
		return err
	})
	require.Error(t, err, "insert with a foreign user_id violates the policy")
	// The superuser pool still sees everything (used by purge and tests).
	n, err := e.q.CountTrades(ctx, b)
	require.NoError(t, err)
	require.EqualValues(t, 3, n)
	// The app role cannot alter the audit log.
	_, err = e.pool.Exec(ctx, "DELETE FROM audit_log")
	require.Error(t, err, "audit_log is append-only for the app role")
}
