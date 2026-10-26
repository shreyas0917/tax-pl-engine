# ADR 0007: Hand-written SQL with sqlc, no ORM

**Status:** accepted · **Date:** 2026-10-26

## Context
The schema has money columns (`NUMERIC(20,4)`), quantity columns (`NUMERIC(20,6)`), civil dates and JSON explanations. Correctness depends on those types surviving the round trip exactly.

## Decision
All queries live in `backend/db/queries/*.sql`; `sqlc` generates typed Go in `internal/store`. Type overrides map `numeric` to `shopspring/decimal`, `uuid` to `string`, `date`/`timestamptz` to `time.Time` and `jsonb` to `[]byte`. The pgx pool registers the shopspring codec so NUMERIC never passes through a float.

## Rationale
- The SQL is visible and reviewable; the cursor-paginated and upsert queries are written once, exactly as Postgres will run them.
- Compile-time checking of every query against the migrations catches schema drift in `make sqlc`, not in production.
- An ORM would hide the `ON CONFLICT ... DO NOTHING` idempotency and the advisory lock behind abstractions that are easy to misuse.

## Consequences
- Adding a column means editing a migration, the query and running `make sqlc`.
- Join queries produce flat row structs (`ListGainsForFYRow`); the handlers map them to API views.
