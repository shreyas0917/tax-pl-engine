# Architecture

```
                 ┌──────────────────────────────────────────────────────────────────┐
                 │  React (Vite, TanStack Query/Table, Tailwind, Motion)            │
                 │  types generated from backend/api/openapi.yaml                   │
                 └───────────────┬──────────────────────────────┬───────────────────┘
                                 │ REST /api/v1 (JSON, problem+json)   │ SSE /jobs/{id}/events
                                 ▼                                     ▼
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ cmd/api  (chi)                                                                      │
│  middleware: request id, structured log, CORS, bearer JWT, rate limit, idempotency  │
│  handlers:  auth, accounts, uploads, jobs, review items, summary, gains, explain,   │
│             trades, holdings, reports, rules                                        │
└───────┬──────────────────────┬─────────────────────────────┬─────────────────────────┘
        │ sqlc queries          │ blob.Store (local | s3)      │ river.InsertTx (same tx)
        ▼                      ▼                             ▼
┌──────────────┐      ┌───────────────┐             ┌──────────────────────────────────┐
│ Postgres 16  │      │ raw files     │             │ cmd/worker (River)               │
│ users, demat │      │ reports       │             │  process_upload: parse+normalize │
│ accounts,    │◄─────┤ purged after  │             │  recompute: per-user advisory    │
│ uploads,     │      │ RAW_FILE_TTL  │             │    lock → pipeline.Run → rebuild │
│ jobs, trades │      └───────────────┘             │    lots/gains/review items       │
│ lots, gains, │                                    │  report: xlsx/pdf/json + checksum│
│ review_items,│◄───────────────────────────────────┤  purge_raw_files (hourly)        │
│ computations,│          jobs.state + job_events    └──────────────┬───────────────────┘
│ audit_log,   │          + audit_log per transition                │
│ river_job    │                                                    ▼
└──────────────┘                                   ┌──────────────────────────────────┐
                                                   │ PURE ENGINE (no I/O)             │
┌──────────────┐                                   │  parser/* → normalize →          │
│ Redis 7      │  rate limits, idempotency keys    │  matcher (+corpaction) →         │
└──────────────┘  (in-memory fallback when absent) │  tax (rules/*.yaml) → explain    │
                                                   │  pipeline.Run orchestrates       │
                                                   └──────────────────────────────────┘
```

## Request flow for an upload

1. `POST /uploads` hashes the file (SHA-256). A hash already seen for this user returns the existing upload (HTTP 200, `duplicate: true`). Otherwise the file goes to the blob store and, in one transaction, an `uploads` row, a `jobs` row (`UPLOADED`), a `job_events` row, an `audit_log` row and a River `process_upload` job are written.
2. The worker's `process_upload` reads the blob, auto-detects the broker (or uses the chosen one), parses, normalises (resolving ISINs), inserts trades with `ON CONFLICT (user_id, source_row_hash) DO NOTHING`, records parse issues as review items, advances the job to `PARSING` → `NORMALIZED`, and enqueues `recompute` in the same transaction.
3. `recompute` takes `pg_advisory_xact_lock(hashtextextended(user_id, 0))`, loads every trade, opening lot, instrument override and corporate action for the user, calls `pipeline.Run` (the same pure function the CLI and the golden tests use), deletes and rebuilds `lots`, `gains` (with the full explanation JSON) and `review_items`, writes a `computations` row with the report checksum, and advances every waiting job through `ADJUSTED` → `MATCHED` → `COMPUTED` → `REPORT_READY` or `NEEDS_REVIEW`.
4. The browser follows `GET /jobs/{id}/events` (SSE, resumable with `Last-Event-ID`); every state change is already durable in `job_events` before it is streamed, so a reconnect never misses a stage.
5. `GET /summary?fy=` runs `tax.Summarise` over the stored gains; `GET /gains/{id}/explain` re-runs the pure pipeline for the user (cheap, deterministic) and returns the lots, trades, adjustments, grandfathering comparison and rule references behind that one line.

## Why the engine is pure

`parser`, `normalize`, `corpaction`, `matcher`, `tax`, `explain` and `pipeline` take values and return values. The CLI (`cmd/taxpl`), the golden tests, the worker and the explain endpoint all call `pipeline.Run`. IDs are content hashes, every slice is sorted by content keys, and decimals replace floats everywhere, so the same inputs and rules version produce a byte-identical report (ADR 0005). Derived tables are therefore disposable: they are always rebuilt, never edited.

## Reliability properties and the tests that prove them

| Property | Mechanism | Test |
|---|---|---|
| Same file twice changes nothing | SHA-256 unique per user; trade rows unique per source-row hash | `TestIntegrationIdempotentUpload` |
| A worker dying mid-job leaves no partial state | every stage writes its outputs and the next job in one transaction; River retries (3 attempts, exponential backoff) | `TestIntegrationInterruptedRecompute` |
| Three concurrent uploads equal three sequential ones | per-user advisory transaction lock; recompute is idempotent | `TestIntegrationConcurrentUploads` |
| Raw files disappear after the TTL, parsed trades stay | hourly `purge_raw_files` job | `TestIntegrationPurgeAndDelete` |
| "Delete my data" removes every row and blob | `DELETE /me` → `jobs.DeleteUserData` (cascade + blob deletes + audit) | `TestIntegrationPurgeAndDelete` |
| Reports are reproducible | checksum recorded; two generations compared | smoke test, golden replay assertions |

## Security notes

- Passwords: bcrypt cost 12. Access tokens: HS256 JWT, 15 minutes. Refresh tokens: opaque, hashed at rest, rotated on use, httpOnly + SameSite=Lax cookie scoped to `/api/v1/auth`.
- CORS only for `FRONTEND_ORIGIN`; uploads capped at `MAX_UPLOAD_MB` with content sniffing (text or XLSX zip only).
- Rate limits per IP (auth endpoints tighter) and per user; Redis-backed with an in-memory fallback.
- PAN is only ever stored as a salted hash. Logs carry request and user IDs, never file contents, emails or tokens.
- Row-level security is forced on every user-owned table (migration 00003). The API connects as the login role and switches each connection to the non-owner role `taxpl_app`; `requireAuth` binds the request's pooled connection to `app.user_id`, and every worker transaction sets it with `set_config(..., true)`. A `taxpl_system` role with BYPASSRLS runs the cross-user raw-file purge and River's own migrations. `taxpl_app` has no UPDATE/DELETE on `audit_log`. `TestIntegrationRowLevelSecurity` proves the isolation.
