# Tax P&L Engine

One FIFO-matched, fully explained capital-gains and F&O report from every Indian broker tradebook you have (Zerodha, Groww, Upstox, Angel One), with a "why this number?" drawer behind every gain line.

![Dashboard](frontend/e2e/screenshots/dashboard-light.png)

| Gains table | Explain drawer ("why this number?") |
|---|---|
| ![Gains](frontend/e2e/screenshots/gains.png) | ![Explain](frontend/e2e/screenshots/explain-drawer.png) |

## The problem

Traders in India commonly use two or three broker apps. Each gives its own tax P&L and none of them combine. At filing time the user has to work out which gains are short-term, which are long-term, which trades were intraday or F&O, what cost applies to shares bought before 1 February 2018, and what happened after splits, bonuses and demergers. This tool does that work and shows how every number was reached.

## What it does

- Parses **Zerodha, Groww, Upstox and Angel One** tradebooks (CSV or XLSX), auto-detects the broker, and reports every rejected row with its line number. Adding a broker is one package plus one import line ([docs/adding-a-broker.md](docs/adding-a-broker.md)).
- Applies splits, bonuses, demergers and mergers to lots on their ex-date.
- Matches every sale FIFO **per demat account** (CBDT Circular 768) with partial-lot consumption and same-day intraday netting.
- Classifies every gain: STCG, LTCG, intraday (speculative), F&O (non-speculative), buyback (three dated regimes).
- Applies 31-Jan-2018 grandfathering per lot, including the post-2018 split adjustment most broker statements get wrong.
- Summarises per year under that year's rules (FY 2023-24 to Tax Year 2026-27 under the Income-tax Act 2025): F&O turnover per the ICAI Guidance Note, loss set-off and carry-forward, and a clearly labelled tax estimate.
- Estimates brokerage, STT and levies from a dated schedule when a tradebook has none, labelled "estimated".
- Reconciles the broker's current holdings statement against the engine's open lots and names every gap.
- Explains every number: sale, matched lots, corporate actions, grandfathering comparison, holding period, rule references and rules version.
- Exports Excel, PDF and JSON reports that carry the rules version, a SHA-256 data fingerprint and the disclaimer.

Supported brokers: Zerodha (Console tradebook), Groww (stocks order history), Upstox (trade report), Angel One (trades and charges).

## Architecture

```
React (Vite, TanStack, Motion)  ──REST / SSE──▶  chi API  ──same transaction──▶  River workers
        ▲ types generated from openapi.yaml            │                                │
        │                                              ▼                                ▼
        └──────────────────────────────────── Postgres 16 (RLS) ◀──── pure engine: parser → normalize →
                                              Redis 7 (limits, idempotency)      corpaction → matcher → tax → explain
                                              blob store (local | S3)
```

The engine is a set of pure Go packages: trades and reference data in, lots, gains and review items out. The CLI, the golden tests, the background worker and the explain endpoint all call the same `pipeline.Run`. Every pipeline stage writes its output and enqueues the next job in one Postgres transaction, progress streams to the browser over SSE, and derived tables are always rebuilt, never edited. Row-level security is forced on every user-owned table. Details and diagram: [docs/architecture.md](docs/architecture.md). Decisions: [docs/adr/](docs/adr/).

## Hard problems solved

- **FIFO per account, never across brokers** (Circular 768), with explicit off-market transfers that carry the original date and cost.
- **Grandfathering after a split**: the 31-Jan-2018 FMV is per original share; the engine divides it by the split factor instead of multiplying it by the post-split quantity.
- **Corporate actions on partially sold lots**: splits, bonuses, demergers (s.49(2C) cost apportionment) and mergers applied on ex-date to whatever is still open.
- **Intraday netting with existing holdings**: min(buy, sell) is speculative; the rest is a delivery sale from the oldest lot.
- **The 12-month boundary**: a sale on the anniversary is long-term (Delhi HC, *Bharti Gupta Ramola*), with calendar-month clamping for leap days.
- **Three buyback regimes** switched by date (s.115QA exempt, deemed dividend with s.46A capital loss, capital gains from 1 April 2026).
- **Idempotency** (same file twice changes nothing), **deterministic replay** (same inputs, same rules version, byte-identical report), and **concurrency safety** (per-user advisory lock).

Write-up with worked numbers: [docs/edge-cases.md](docs/edge-cases.md).

## How correctness is proven

- **35 golden cases** in `backend/testdata/golden/`, each with a hand-computed `expected.json` and a note explaining the arithmetic.
- **Property tests** (`pgregory.net/rapid`) on the matcher: never sells more than bought, lot quantities and costs never negative, cost conserved to the paisa, FIFO order within a sale, intraday quantity equals min(buy, sell), results independent of input order, accounts independent of each other.
- **Holding-period property test**: the long-term rule is monotonic and agrees with day counts away from the boundary; the boundary itself is pinned by golden cases 04 and 32.
- **Replay**: every golden case runs twice and asserts a byte-identical report checksum; two report generations in the live system compare equal.
- **No floats**: a test fails the build if `float32` or `float64` appears in any pure engine package; money is `shopspring/decimal` end to end.
- **Rules are data with sources**: [docs/tax-rules.md](docs/tax-rules.md) lists every rule, its statutory or practitioner source, and the test that proves it. Each rules file has a `verified_by` field for a Chartered Accountant's sign-off, which is still pending.
- **Reliability against a real Postgres** (testcontainers): uploading the same file twice adds nothing; a worker interrupted mid-recompute leaves no partial state and the retry completes; three uploads processed concurrently equal three processed one by one; raw files are purged after the TTL while trades stay; "delete my data" removes every row and blob.
- **Tenant isolation**: `TestIntegrationRowLevelSecurity` proves one user cannot read, count or write another user's rows and that the audit log is append-only for the application role.
- **Frontend**: Vitest unit tests and a Playwright flow (register, upload two broker files, watch the pipeline over SSE, open the Explain drawer, resolve a review item, generate and download an Excel report).

## Benchmarks

Pure engine, in-process, single machine (Windows 11, Go 1.26). Full table and method in [docs/benchmarks.md](docs/benchmarks.md).

| Trades | Match (FIFO + netting) | Classify + summarise | Full pipeline | Peak heap |
|---:|---:|---:|---:|---:|
| 100,000 | 3.55 s | 2.01 s | 5.89 s | 354 MB |
| 250,000 | 11.33 s | 5.31 s | 26.53 s | 725 MB |
| 500,000 | 21.03 s | 12.61 s | 36.04 s | 658 MB |

## Run locally

**Option 1: everything in Docker** (needs only Docker Desktop):

```sh
cp .env.example .env
make app         # builds api, worker and web images; runs migrations; starts Postgres and Redis
```

Open http://localhost:8081. `make app-down` stops the containers and keeps the data.

**Option 2: dev workflow** (Go 1.26+ and Node 24+ on the host, hot reload):

```sh
cp .env.example .env
make up          # Postgres + Redis in Docker
make migrate     # goose migrations (incl. row-level security)
make api         # terminal 1
make worker      # terminal 2
make web         # terminal 3: http://localhost:5173
```

`make check` runs lint plus every engine test (no Docker needed); `make test-integration` runs the Postgres-backed tests. On Windows use Git Bash; `make` comes from `winget install GnuWin32\bin` to PATH) or run the Makefile commands directly.

Try the engine without the server:

```sh
cd backend
go run ./cmd/seed -out ../.data/seed -trades 500 -fno 40
go run ./cmd/taxpl calculate --fy 2025-26 --estimate-charges ../.data/seed/zerodha_account1_tradebook.csv ../.data/seed/zerodha_account1_fno.csv
go run ./cmd/taxpl rules
```

## Tech stack

Go 1.26, chi, pgx + sqlc, goose, River, shopspring/decimal, excelize, go-pdf/fpdf, testcontainers-go, rapid, golangci-lint · React 19, TypeScript strict, Vite, React Router, TanStack Query/Table/Virtual, Tailwind v4, Radix, Motion, Recharts, React Hook Form + Zod, openapi-typescript, Vitest, Playwright · Postgres 16, Redis 7, Docker Compose (MinIO and Kafka behind profiles).

## Disclaimer

This tool helps organise data. It is **not tax advice**. The tax rules it applies were researched from published sources on 26 October 2026 and have not yet been signed off by a Chartered Accountant; verify every figure with a qualified CA before filing.

## Author

**Shreyas Wani** · [GitHub](https://github.com/shreyas0917)
