# Tax P&L Engine — Build Specification

*Spec version 2 · tax rules researched on 26 Oct 2026 (Section 6.11) · covers FY 2024-25, FY 2025-26 and Tax Year 2026-27 under the new Income-tax Act, 2025*

> **For the AI coding agent reading this file:** this is the single source of truth for the project. Read the whole file before writing any code. Build **one phase at a time**, in order. Do not start a phase until the previous phase meets every item in its "Definition of done". When something here is ambiguous, stop and ask the owner instead of guessing. Never change tax logic without a test that proves the change.

---

## 0. How to use this file (for the owner)

1. Create an empty folder `taxpl/`, run `git init`, and put this file at the root as `TAXPL_BUILD_SPEC.md`.
2. Set your own git identity before anything else (Section 15 explains why):
   ```bash
   git config user.name "Shreyas Wani"
   git config user.email "<your email>"
   ```
3. Create `CLAUDE.md` at the root with exactly this content:
   ```markdown
   Read TAXPL_BUILD_SPEC.md before every task and follow it strictly.
   Work one phase at a time. Current phase: 1.
   Never add AI attribution to commits or PRs (see Section 15 of the spec).
   ```
   Update "Current phase" yourself as you finish each phase.
4. Create `.claude/settings.json` (Section 15) so no AI co-author line is ever added.
5. Start Docker Desktop, open Claude Code in the folder, and say:
   *"Read TAXPL_BUILD_SPEC.md. Start Phase 0, then Phase 1. Show me the plan before writing code."*

---

## 1. The project in simple words

Many Indian traders use 2–3 apps (Zerodha, Groww, Upstox, Angel One). At tax time, each app gives its own report and nobody combines them. People struggle to answer:

- Which of my profits are **short-term** and which are **long-term**?
- Which trades were **intraday** (taxed as speculative business income) and which were **F&O** (non-speculative business income)?
- For shares bought **before 1 Feb 2018**, what cost do I use (the **grandfathering** rule)?
- What happens after a **split, bonus or demerger**?

**Tax P&L Engine** lets a user upload the tradebook files from every broker. It:

1. Reads each broker's file format (a "parser" per broker).
2. Converts all trades into one standard format.
3. Adjusts for corporate actions (splits, bonus, demergers).
4. Matches every sale to the shares it sold using **FIFO, per demat account**.
5. Classifies every gain: STCG, LTCG, intraday, F&O.
6. Produces a clean report the user (or their CA) can use to file the ITR.
7. Explains every number: for any gain, the user can click **"Why this number?"** and see the exact buy lots, dates, costs and rules used.

It is a **backend-heavy** project. The frontend exists to make the backend's work visible and trustworthy.

**Important disclaimer (must appear in the app and README):** this tool helps organise data. It is not tax advice. Users should verify with a qualified CA.

---

## 2. Rating target: what makes this 9+/10

Every phase must move toward these. Treat them as the acceptance bar for the whole project.

| # | Requirement | Where it is built |
|---|---|---|
| 1 | **Correctness is proven**, not claimed: golden-file tests with hand-verified answers | Phase 1 onward |
| 2 | **Property tests** on FIFO (e.g. never sells more than was bought) | Phase 1 |
| 3 | **Exact money math**: `shopspring/decimal` in Go, `NUMERIC` in Postgres, never `float64` | Everywhere |
| 4 | **Idempotency**: same file uploaded twice changes nothing; retries never double-count | Phase 2 |
| 5 | **Deterministic replay**: same inputs + same rules version → byte-identical report | Phase 3 |
| 6 | **Explainability**: "Why this number?" for every gain | Phase 3 |
| 7 | **Hard real-world edge cases**: grandfathering, splits, bonus, demergers, partial sells, intraday netting | Phase 4 |
| 8 | **Concurrency safety**: one user uploading 3 files at once gives the same result as uploading them one by one | Phase 4 |
| 9 | **Benchmark numbers** in the README (e.g. "100k trades processed in X s") | Phase 5 |
| 10 | **Polished, fast, accessible frontend** built with the design skills in Section 12 | Phase 3 onward |
| 11 | **Excellent README + architecture docs + a write-up of edge cases solved** | Every phase |
| 12 | **Real users**: the owner shares it with real traders | After Phase 3 |

---

## 3. Tech stack (fixed — do not substitute without asking)

### Backend — Go
| Concern | Choice |
|---|---|
| Language | Go (latest stable) |
| HTTP router | `go-chi/chi/v5` |
| Postgres driver | `jackc/pgx/v5` (pgxpool) |
| SQL | **`sqlc`** — hand-written SQL, generated type-safe Go. **No ORM, no GORM.** |
| Migrations | `pressly/goose` (SQL migrations in `backend/db/migrations`) |
| Money | `shopspring/decimal` |
| CSV / Excel input | `encoding/csv`, `xuri/excelize/v2` |
| Excel report output | `xuri/excelize/v2` |
| PDF report output | `go-pdf/fpdf` |
| Background jobs | **River** (`riverqueue/river`) — Postgres-backed job queue (Phases 2–4) |
| Events (Phase 5 only) | Kafka via `twmb/franz-go`, fed by a transactional outbox |
| Cache / rate limit / idempotency keys | Redis via `redis/go-redis/v9` |
| File storage | `BlobStore` interface with two drivers: `local` (default, files on disk) and `s3` (MinIO in dev) |
| Auth | Email + password (`golang.org/x/crypto/bcrypt` or argon2id), short-lived JWT access token (`golang-jwt/jwt/v5`) + refresh token in an httpOnly cookie |
| Config | Environment variables, loaded into one typed `Config` struct at startup; fail fast if missing |
| Logging | `log/slog` (JSON in prod, text in dev), request ID on every log line |
| Validation | Explicit validation functions; return RFC 9457 `application/problem+json` errors |
| API contract | OpenAPI 3.1 spec in `backend/api/openapi.yaml` — written first, kept in sync |
| Tests | `go test`, `stretchr/testify`, `testcontainers-go` (real Postgres), golden files, `pgregory.net/rapid` for property tests |
| Lint | `golangci-lint` |

### Frontend — React
| Concern | Choice |
|---|---|
| Build | Vite + React (latest stable) + **TypeScript (strict)** |
| Routing | React Router |
| Server state | TanStack Query |
| Tables | TanStack Table |
| Styling | Tailwind CSS |
| Components | shadcn/ui (Radix primitives) as a base, restyled to our design system |
| Animation | **Motion** (formerly Framer Motion): package `motion`, import from `motion/react` |
| Charts | Recharts |
| Forms | React Hook Form + Zod |
| API types | Generated from `openapi.yaml` with `openapi-typescript` — never hand-write API types |
| File upload | `react-dropzone` |
| Tests | Vitest + Testing Library; Playwright for 3–5 end-to-end flows |

### Local environment — Docker Desktop
- Postgres and Redis always run in Docker (Section 5).
- MinIO (S3) and Kafka are optional, behind Compose **profiles**.
- The Go API, Go worker and React dev server run on the host with hot reload.
- **No DevOps scope**: no Kubernetes, Terraform, cloud IaC, CI/CD pipelines or blue-green deploys. Deployment, if done later, is a one-click host (Render / Railway / Fly.io).

---

## 4. Repository structure

```
taxpl/
├── TAXPL_BUILD_SPEC.md          # this file
├── CLAUDE.md
├── README.md                    # owner-facing, polished (Section 14)
├── docker-compose.yml
├── .env.example
├── Makefile                     # all common commands (Windows: run from Git Bash or WSL)
├── .githooks/commit-msg         # strips AI attribution lines (Section 15)
├── .claude/settings.json        # attribution off (Section 15)
├── docs/
│   ├── architecture.md          # diagrams + explanation
│   ├── tax-rules.md             # every rule implemented, with source and test reference
│   ├── edge-cases.md            # write-up of hard cases solved (for interviews/blog)
│   ├── benchmarks.md
│   └── adr/                     # Architecture Decision Records, one per big decision
├── backend/
│   ├── go.mod
│   ├── api/openapi.yaml
│   ├── cmd/
│   │   ├── taxpl/               # CLI (Phase 1): taxpl calculate <files...>
│   │   ├── api/                 # HTTP server
│   │   ├── worker/              # River workers
│   │   └── seed/                # synthetic tradebook generator for demos/tests
│   ├── db/
│   │   ├── migrations/
│   │   ├── queries/             # .sql files for sqlc
│   │   └── sqlc.yaml
│   ├── internal/
│   │   ├── domain/              # Trade, Lot, LotMatch, Gain, Money helpers, enums
│   │   ├── parser/              # Parser interface + registry
│   │   │   ├── zerodha/
│   │   │   ├── groww/
│   │   │   ├── upstox/
│   │   │   └── angelone/
│   │   ├── normalize/
│   │   ├── corpaction/
│   │   ├── matcher/             # FIFO engine (pure, no I/O)
│   │   ├── tax/                 # classifier + rules loader
│   │   │   └── rules/           # fy2023-24.yaml, fy2024-25.yaml, fy2025-26.yaml ...
│   │   ├── charges/
│   │   ├── report/              # excel, pdf, json writers
│   │   ├── explain/             # builds "why this number" trees
│   │   ├── pipeline/            # orchestrates stages, job state machine
│   │   ├── store/               # sqlc generated code + repository helpers
│   │   ├── blob/                # BlobStore: local + s3
│   │   ├── auth/
│   │   ├── httpapi/             # handlers, middleware, problem+json errors
│   │   ├── audit/
│   │   └── config/
│   └── testdata/
│       ├── golden/              # <case>/input/*.csv + <case>/expected.json
│       └── private/             # real user files — GITIGNORED, never committed
└── frontend/
    ├── package.json
    ├── src/
    │   ├── api/                 # generated types + typed client
    │   ├── components/          # design-system components
    │   ├── features/            # upload, dashboard, gains, explain, review, reports, settings
    │   ├── lib/                 # formatting (INR, dates), motion presets
    │   ├── routes/
    │   └── styles/
    └── e2e/                     # Playwright
```

**Core rule:** `parser`, `normalize`, `corpaction`, `matcher`, `tax`, `charges` and `explain` are **pure Go packages with no database or network calls**. They take data in and return data out. This keeps them fast to test and makes deterministic replay possible. All I/O lives in `pipeline`, `store`, `blob` and `httpapi`.

---

## 5. Docker Compose (create this file exactly, then extend only if needed)

`docker-compose.yml`:

```yaml
name: taxpl

services:
  postgres:
    image: postgres:16-alpine
    container_name: taxpl-postgres
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-taxpl}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-taxpl_dev_password}
      POSTGRES_DB: ${POSTGRES_DB:-taxpl}
    ports:
      - "${POSTGRES_PORT:-5432}:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $${POSTGRES_USER} -d $${POSTGRES_DB}"]
      interval: 5s
      timeout: 3s
      retries: 10

  redis:
    image: redis:7-alpine
    container_name: taxpl-redis
    command: ["redis-server", "--appendonly", "yes"]
    ports:
      - "${REDIS_PORT:-6379}:6379"
    volumes:
      - redisdata:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 10

  # Optional S3-compatible storage. Start with: docker compose --profile s3 up -d
  minio:
    image: minio/minio:latest
    container_name: taxpl-minio
    profiles: ["s3"]
    command: server /data --console-address ":9001"
    environment:
      MINIO_ROOT_USER: ${MINIO_ROOT_USER:-taxpl}
      MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD:-taxpl_dev_password}
    ports:
      - "9000:9000"
      - "9001:9001"
    volumes:
      - miniodata:/data

  minio-init:
    image: minio/mc:latest
    profiles: ["s3"]
    depends_on:
      - minio
    environment:
      MINIO_ROOT_USER: ${MINIO_ROOT_USER:-taxpl}
      MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD:-taxpl_dev_password}
    entrypoint: >
      /bin/sh -c "
      until mc alias set local http://minio:9000 $${MINIO_ROOT_USER} $${MINIO_ROOT_PASSWORD}; do sleep 1; done;
      mc mb --ignore-existing local/taxpl-raw;
      mc mb --ignore-existing local/taxpl-reports;
      exit 0;
      "

  # Optional event bus for Phase 5. Start with: docker compose --profile events up -d
  kafka:
    image: apache/kafka:3.8.0
    container_name: taxpl-kafka
    profiles: ["events"]
    ports:
      - "9092:9092"

volumes:
  pgdata:
  redisdata:
  miniodata:
```

`.env.example` (copy to `.env`; `.env` is gitignored):

```dotenv
# Infrastructure
POSTGRES_USER=taxpl
POSTGRES_PASSWORD=taxpl_dev_password
POSTGRES_DB=taxpl
POSTGRES_PORT=5432
REDIS_PORT=6379

# App
APP_ENV=dev
HTTP_ADDR=:8080
DATABASE_URL=postgres://taxpl:taxpl_dev_password@localhost:5432/taxpl?sslmode=disable
REDIS_URL=redis://localhost:6379/0
JWT_SECRET=change-me-to-a-long-random-string
FRONTEND_ORIGIN=http://localhost:5173

# File storage: local (default) or s3
BLOB_DRIVER=local
BLOB_LOCAL_DIR=./.data/blobs
S3_ENDPOINT=http://localhost:9000
S3_ACCESS_KEY=taxpl
S3_SECRET_KEY=taxpl_dev_password
S3_BUCKET_RAW=taxpl-raw
S3_BUCKET_REPORTS=taxpl-reports

# Retention
RAW_FILE_TTL_HOURS=24

# Phase 5 only
KAFKA_BROKERS=localhost:9092
```

Notes:
- If port 5432 is already used by a local Postgres install, set `POSTGRES_PORT=5433` and update `DATABASE_URL`.
- If the MinIO image cannot be pulled, keep `BLOB_DRIVER=local`; the app must work fully without MinIO.

**Makefile targets (required):** `up`, `down`, `reset-db`, `migrate`, `sqlc`, `api`, `worker`, `web`, `test`, `test-golden`, `lint`, `seed`, `bench`, `check` (lint + all tests), `hooks` (installs git hooks).

---

## 6. Domain rules (the heart of the project)

> **Rule for the agent:** every rule below must be implemented as data or as a small pure function, documented in `docs/tax-rules.md` with its source, and covered by at least one golden test. Never hardcode a rate, threshold, date or section number inside business logic — read it from the per-year rules file (6.9).
>
> **Status tags used below** (research done on 26 Oct 2026, sources in 6.11):
> - **[VERIFIED]** — confirmed from current published sources. Still get the owner's CA to sign off before release.
> - **[ASK CA]** — sources are unclear or silent. Implement behind a config flag, add a golden test with the CA's worked answer, and do not release until the owner marks it verified in `docs/tax-rules.md`.

### 6.1 Basic definitions and the new Income-tax Act
- **Year:** 1 April to 31 March. A sale belongs to the year of its **trade date**.
- **Two laws, one engine** [VERIFIED]:
  - Sales up to **31 March 2026** (FY 2025-26 and earlier) fall under the **Income-tax Act, 1961**, using "previous year / assessment year".
  - Sales from **1 April 2026** (Tax Year 2026-27 onward) fall under the **Income-tax Act, 2025**, which replaces both terms with a single **"Tax Year"**.
  - Section numbers changed in the new Act (for example, old 44AB became 63 and old 48 became 72), and published mapping tables don't all agree. **Never hardcode section numbers.** Each year's rules file stores its own `law` and `references`, and the UI and reports show whatever that file says. Old-Act section names below are given only to help you search.
- In the UI, label years as "FY 2025-26" for old-Act years and "Tax Year 2026-27" for new-Act years, both driven by the rules file.
- **Demat account:** a user can have several (usually one per broker). Every trade belongs to exactly one account.
- **ISIN** is the identity of a security. Symbols change; ISINs mostly do not. Map symbol → ISIN via the reference table; anything unmapped becomes a review item.

### 6.2 Categories the engine must produce
| Category | What it is | Notes |
|---|---|---|
| `STCG_EQUITY` | STT-paid listed equity shares / equity-oriented MF units held **12 months or less** | Old Act s.111A [VERIFIED] |
| `LTCG_EQUITY` | Same, held **more than 12 months** | Old Act s.112A [VERIFIED] |
| `INTRADAY` | Bought and sold the same ISIN, same account, same trading day | Speculative business income [VERIFIED] |
| `FNO` | Futures and options | Non-speculative business income, reported in ITR-3 business schedule [VERIFIED] |
| `BUYBACK` | Shares tendered in a company buyback | Capital gains from 1 Apr 2026; deemed dividend before that (6.6) [VERIFIED for the change, ASK CA for reporting] |
| `OUT_OF_SCOPE` | Debt MFs, bonds, unlisted shares, SGBs, etc. | Shown to the user, excluded from calculations in v1 |

### 6.3 Holding period
- Long-term if held **more than 12 months** [VERIFIED].
- Implement as `sale_date > acquisition_date + 12 calendar months`, with the exact comparison in one function `IsLongTerm(acq, sale)`.
- The exact boundary is **[ASK CA]**: for a buy on 10 Aug 2024, is a sale on 10 Aug 2025 short-term and 11 Aug 2025 long-term? Make the boundary a golden test using the CA's answer, plus property tests on both sides of it.
- Bonus shares and split/demerger shares follow 6.6 for their acquisition date.

### 6.4 FIFO matching
- **FIFO is applied per demat account and per ISIN** [VERIFIED: CBDT Circular No. 768 dated 24 June 1998 says FIFO applies to each demat account separately]. It is **not** applied across brokers. The product's value is consolidating many accounts into one report, not mixing their lots.
- Order of lots: trade date, then exchange order/trade time if present, then a stable tie-breaker (source row order).
- A sale may consume part of one lot and part of the next. Lots support partial consumption.
- **Intraday first:** for each (account, ISIN, trading day), match same-day buys and sells first as `INTRADAY` up to `min(buy_qty, sell_qty)`; the remainder goes into delivery FIFO. This is how most brokers' tax reports do it, but the exact split when the user already holds shares is **[ASK CA]** (CA example C). Keep the netting logic in one function behind a config flag.
- **Off-market transfers between the user's own accounts** (Phase 4): the receiving account gets a lot that keeps the original acquisition date and cost. If unknown, create a review item asking for them. **[ASK CA]** to confirm.
- A sell with no matching buy history (e.g. shares bought years ago on another platform) → `NEEDS_REVIEW` item: "We couldn't find the purchase for 40 shares of X. Enter the buy date and price, or upload the older tradebook."

### 6.5 Grandfathering (shares acquired on or before 31 Jan 2018) [VERIFIED]
For STT-paid listed equity and equity-oriented MF units acquired **on or before 31 Jan 2018** and sold as long-term:

```
cost_of_acquisition = max(actual_cost, min(FMV_on_31_Jan_2018, sale_value))
```

- **FMV = the highest traded price on a recognised stock exchange on 31 Jan 2018** (for MF units, the NAV on that date). Store it in `ref.instruments.fmv_2018_01_31`.
- Applies per lot, using total values (FMV × quantity vs sale value × quantity). The explain tree must show both values and which was chosen.
- **Split after 2018 (important, commonly done wrong):** the FMV is per **original** share. After a split, divide the FMV by the split ratio. A 2:1 split turns an FMV of ₹605 into ₹302.50 per share, so total FMV stays the same. Make this a dedicated golden test; many broker statements multiply the unsplit FMV by the post-split quantity, which overstates cost.
- **Bonus shares allotted on or before 31 Jan 2018:** treatment is **[ASK CA]**. Implement behind a flag, defaulting to the CA's answer.

### 6.6 Corporate actions
| Action | Effect on lots | Status |
|---|---|---|
| **Split** (e.g. 1:5) | Quantity × ratio, cost per share ÷ ratio, total cost unchanged, **acquisition date unchanged**; grandfathered FMV per share ÷ ratio | [VERIFIED] |
| **Bonus** (e.g. 1:1) | New lot of bonus shares with **cost = 0**, acquisition date = allotment date, holding period counted separately from the original shares. Original lot unchanged. | [VERIFIED]; pre-2018 bonus grandfathering [ASK CA] |
| **Demerger** | Original cost is apportioned between parent and new company using the cost-apportionment ratio the company announces; acquisition date carries over | Approach standard; source of ratio and exact method [ASK CA] |
| **Merger / symbol change** | Lot moves to the new ISIN with the same date and cost (and the announced swap ratio) | [ASK CA] to confirm |
| **Buyback** (tender offer) | Sale **on or before 31 Mar 2026**: old regime (treat as deemed dividend; show it, flagged). Sale **from 1 Apr 2026**: taxed as **capital gains** (proceeds minus cost of acquisition), normal STCG/LTCG by holding period. Promoters face an additional buyback tax, which is out of scope. | Change [VERIFIED]; ITR reporting details [ASK CA] |

Corporate actions must be applied **before** matching, ordered by ex-date, and every adjustment must be written to `audit_log`.

### 6.7 Charges
- Tradebooks usually don't contain charges. Support two sources:
  1. **Imported**: the broker's P&L / contract-note export (preferred).
  2. **Estimated**: a per-broker charge schedule in config, with **date-based rates**. The UI must label these as **"Estimated"**.
- **Capital gains** [VERIFIED]: brokerage on purchase is **added to cost**, and brokerage on sale is **deducted from sale value** (expenses wholly in connection with the transfer). **STT is not deductible** for capital gains.
- **Intraday and F&O (business income)** [VERIFIED]: charges including **STT, brokerage and exchange charges are deductible business expenses**.
- **STT rates on F&O changed from 1 April 2026** [VERIFIED] (store as dated entries, never constants):

  | Instrument | Up to 31 Mar 2026 | From 1 Apr 2026 | Paid by |
  |---|---|---|---|
  | Futures (sale) | 0.02% | 0.05% | Seller |
  | Options (sale, on premium) | 0.10% | 0.15% | Seller |
  | Options (exercised) | 0.125% | 0.15% | Buyer |

  Equity delivery and intraday STT were not changed by Budget 2026. Look up the exact current delivery/intraday rates when building the charge schedule, and cite the source in `docs/tax-rules.md`.

### 6.8 F&O
- P&L per contract (underlying + expiry + strike + option type) using FIFO within the account.
- **Turnover** [VERIFIED]: the **sum of absolute profits and losses** across trades (a ₹50,000 profit and a ₹30,000 loss give ₹80,000 turnover). Since ICAI's 2022 Guidance Note revision, **option premium is not added separately**. Turnover is **not** the contract/notional value.
- **Tax audit threshold** (old Act s.44AB, new Act s.63) [VERIFIED]: ₹1 crore turnover, raised to **₹10 crore** when cash receipts and payments are each ≤ 5% (usually true for F&O). A loss by itself does not trigger an audit; a separate trigger can apply to people who used presumptive taxation earlier. **The app must not tell users whether they need an audit.** Show turnover and the thresholds, and say "check with your CA".
- Show turnover prominently on the dashboard and in reports.

### 6.9 Rules as versioned data
Store one file per year in `backend/internal/tax/rules/`. The values below are the verified current values; still keep `verified_by` empty until the owner's CA signs off.

`fy2024-25.yaml`
```yaml
year: "2024-25"
law: "Income-tax Act, 1961"
year_label: "FY 2024-25 (AY 2025-26)"
version: 1
equity:
  long_term_after_months: 12
  periods:                         # rates changed mid-year: split by transfer (sale) date
    - from: "2024-04-01"
      to: "2024-07-22"
      stcg_rate: "0.15"
      ltcg_rate: "0.10"
    - from: "2024-07-23"
      to: "2025-03-31"
      stcg_rate: "0.20"
      ltcg_rate: "0.125"
  ltcg_exemption: "125000"         # [ASK CA] how the limit is shared between the two periods
  report_split_by_date: "2024-07-23"   # ITR reports pre/post sales separately
grandfathering:
  cutoff_acquisition_date: "2018-01-31"
references:
  stcg: "s.111A"
  ltcg: "s.112A"
  audit: "s.44AB"
verified_by: ""
```

`fy2025-26.yaml`
```yaml
year: "2025-26"
law: "Income-tax Act, 1961"
year_label: "FY 2025-26 (AY 2026-27)"
version: 1
equity:
  long_term_after_months: 12
  periods:
    - from: "2025-04-01"
      to: "2026-03-31"
      stcg_rate: "0.20"
      ltcg_rate: "0.125"
  ltcg_exemption: "125000"
grandfathering:
  cutoff_acquisition_date: "2018-01-31"
buyback_regime: "deemed_dividend"
references:
  stcg: "s.111A"
  ltcg: "s.112A"
  audit: "s.44AB"
verified_by: ""
```

`ty2026-27.yaml`
```yaml
year: "2026-27"
law: "Income-tax Act, 2025"
year_label: "Tax Year 2026-27"
version: 1
equity:
  long_term_after_months: 12
  periods:
    - from: "2026-04-01"
      to: "2027-03-31"
      stcg_rate: "0.20"            # unchanged by Budget 2026
      ltcg_rate: "0.125"           # unchanged by Budget 2026
  ltcg_exemption: "125000"         # unchanged by Budget 2026
grandfathering:
  cutoff_acquisition_date: "2018-01-31"   # [ASK CA] confirm carried into the 2025 Act
buyback_regime: "capital_gains"
references:                        # [ASK CA] confirm new-Act section numbers; sources conflict
  stcg: ""
  ltcg: ""
  audit: "s.63"
verified_by: ""
```

- Every job records the `rules_version` it used (e.g. `ty2026-27@1`), so an old report can always be regenerated exactly.
- Note: a gain under old s.112A is **not** eligible for the s.87A rebate, so tax on it can be payable even when total income is low. Show this as an info note in the tax estimate, not as advice.
- The primary output is **classified gains** (what the ITR schedules need). Estimated tax amounts are a secondary feature, clearly labelled "estimate".

### 6.10 Losses: set-off and carry forward (Phase 4 feature: "Loss summary")
Show users what they can carry forward. Implement as rules data, not code branches. [VERIFIED]

| Loss | Same-year set-off against | Carry forward |
|---|---|---|
| Short-term capital loss (STCL) | STCG and LTCG | 8 years, against capital gains |
| Long-term capital loss (LTCL) | LTCG only | 8 years, against LTCG only |
| Intraday (speculative) loss | Speculative income only | 4 years, against speculative income only |
| F&O (non-speculative business) loss | Any income **except salary** | 8 years, against business income only |

- Carry forward is allowed **only if the return is filed by the due date** (show this warning).
- Capital losses cannot be set off against salary or other non-capital income.
- Some older articles say long-term losses on STT-paid shares cannot be set off; that applied before 2018 and is out of date. Follow this table.

### 6.11 Sources (checked 26 Oct 2026; add each to `docs/tax-rules.md`)
- Equity rates, holding period, ₹1.25 lakh exemption: ClearTax — https://cleartax.in/s/long-term-capital-gains-ltcg-tax ; Tata MF post-Budget 2026 — https://www.tatamutualfund.com/blogs/post-budget-2026-new-tax-map-equity-investors
- 23 July 2024 rate change and pre/post split: TaxBuddy — https://www.taxbuddy.com/blog/new-capital-gain-tax
- Budget 2026 (STT on F&O, buybacks, no rate changes): Business Standard — https://www.business-standard.com/budget/news/how-budget-2026-impacts-your-investments-stocks-gold-and-mutual-funds-126020200254_1.html ; Motilal Oswal — https://www.motilaloswal.com/learning-centre/2026/2/top-budget-2026-announcements-for-the-stock-market
- Income-tax Act 2025, Tax Year, section mapping (conflicting tables): TaxGuru — https://taxguru.in/income-tax/income-tax-act-2025-1961-section-mapping-cheat-sheet-sections-day.html
- FIFO per demat account (Circular 768): Business Standard — https://www.business-standard.com/finance/personal-finance/explained-how-to-calculate-capital-gains-tax-on-multiple-demat-accounts-124080200149_1.html ; Income Tax Department — https://www.incometaxindia.gov.in/w/768-circular-no.-768-dated-24-6-1998-1
- Grandfathering formula and FMV: Geojit — https://support.geojit.com/support/solutions/articles/89000006851 ; split handling: Freefincal — https://freefincal.com/how-to-calculate-ltcg-with-grandfathering-for-equity-shares-that-split-how-to-fill-schedule-112a/
- Bonus shares: Geojit — https://support.geojit.com/support/solutions/articles/89000006864
- STT and brokerage treatment: Court Kutchehry — https://courtkutchehry.com/blog/capital-gains-taxation-brokerage-stt-investment-deductions ; Freefincal — https://freefincal.com/stock-trading-taxation/
- F&O turnover and audit thresholds: 1 Finance — https://1finance.co.in/blog/fo-turnover-calculation-income-tax/
- Losses: m.Stock — https://www.mstock.com/articles/short-term-vs-long-term-capital-loss-set-off-carry-forward ; Upstox — https://upstox.com/market-talk/set-off-and-carry-forward-of-losses-on-shares/

Secondary sources are fine for building. Before release, the owner's CA should confirm against the Finance Act / Income-tax Act text and the official CBDT section concordance.

### 6.12 Open questions for the owner's CA (all [ASK CA] items)
1. Exact 12-month boundary date (6.3).
2. How the ₹1.25 lakh LTCG exemption applies across the 23 July 2024 split in FY 2024-25.
3. Intraday netting when the user already holds shares: hold 100, buy 50 and sell 80 the same day.
4. Off-market transfer between own demat accounts: do the original date and cost carry over?
5. Grandfathering for bonus shares allotted before 1 Feb 2018.
6. Demerger and merger cost apportionment: where to source official ratios.
7. Buyback reporting in the ITR: before vs from 1 April 2026.
8. Correct section numbers in the Income-tax Act 2025 for the STCG, LTCG, grandfathering, speculative and loss provisions.
9. Whether grandfathering continues unchanged under the 2025 Act.
10. The CA's hand-calculated answers for the five example trades. These become golden tests `ca_example_a` … `ca_example_e`.

---

## 7. Data model (PostgreSQL)

Write migrations with goose. Use `NUMERIC(20,4)` for money and `NUMERIC(20,6)` for quantities, `timestamptz` for timestamps, `uuid` primary keys (`gen_random_uuid()`), and `created_at` on everything.

Core tables (columns are the minimum; add what the code needs and document it):

- `users(id, email UNIQUE, password_hash, pan_hash NULL, created_at)`
- `demat_accounts(id, user_id, broker, nickname, created_at)` — UNIQUE(user_id, broker, nickname)
- `uploads(id, user_id, account_id, broker, filename, sha256, size_bytes, blob_key, status, created_at)` — **UNIQUE(user_id, sha256)** gives idempotent re-uploads
- `jobs(id, user_id, upload_id NULL, kind, state, rules_version, attempt, error JSONB NULL, started_at, finished_at, updated_at)`
- `trades(id, user_id, account_id, upload_id, isin, symbol, segment, side, quantity, price, trade_date, trade_time NULL, exchange, order_id, trade_ref, source_row_hash UNIQUE, raw JSONB)` — `source_row_hash` makes re-processing idempotent
- `corporate_action_applications(id, user_id, account_id, corporate_action_id, applied_at)`
- `lots(id, user_id, account_id, isin, open_trade_id NULL, origin, quantity_original, quantity_open, cost_total, acquisition_date, grandfathered_fmv NULL)`
- `lot_matches(id, user_id, sell_trade_id, lot_id, quantity, cost_basis, sale_value, holding_days, category)`
- `gains(id, user_id, lot_match_id NULL, fno_contract_key NULL, fy, category, gain, charges_deductible, rules_version)`
- `charges(trade_id PK, brokerage, stt, exchange_fee, sebi_fee, gst, stamp_duty, source)` — source = `imported` | `estimated`
- `review_items(id, user_id, job_id, kind, payload JSONB, status, resolution JSONB NULL, created_at, resolved_at)`
- `reports(id, user_id, job_id, fy, format, blob_key, checksum, rules_version, created_at)`
- `audit_log(id BIGSERIAL, user_id, entity, entity_id, action, before JSONB, after JSONB, at)` — append-only; the app's DB role has no UPDATE/DELETE grant on it
- `ref.instruments(isin PK, symbol, name, exchange, segment, fmv_2018_01_31 NULL)`
- `ref.corporate_actions(id, isin, kind, ratio_from, ratio_to, cost_split_pct NULL, new_isin NULL, ex_date, source)`
- River's own tables (created by River's migrator)

Rules:
- Every user-owned table has `user_id` and an index on it. Enable **row-level security** on user-owned tables in Phase 4 and set `app.user_id` per transaction.
- Derived tables (`lots`, `lot_matches`, `gains`) are **recomputed**, never edited by hand: for an affected (account, ISIN), delete derived rows and rebuild them inside one transaction. This is what makes replay deterministic.

---

## 8. Pipeline and job state machine

States: `UPLOADED → PARSING → NORMALIZED → ADJUSTED → MATCHED → COMPUTED → REPORT_READY`, plus `NEEDS_REVIEW` and `FAILED`.

- Each stage is a River job. A stage reads its input from Postgres, runs a **pure** function, and writes its output plus the next job **in the same transaction**.
- Every state change writes `jobs.state`, an `audit_log` row and a progress event (pushed to the browser over **SSE**).
- Retries: 3 attempts with exponential backoff, then `FAILED` with a readable error the UI can show.
- **Per-user serialization (Phase 4):** recomputation for a user takes `pg_advisory_xact_lock(hashtextextended(user_id::text, 0))` so concurrent uploads queue up instead of corrupting lots. Prove it with a test that fires 3 uploads concurrently and compares the result with sequential uploads.

### Parser plugin interface

```go
type Parser interface {
    Broker() string
    // Detect returns a confidence 0..1 that this file belongs to this broker/format.
    Detect(header []string, sample [][]string) float64
    // Parse returns raw rows with source line numbers; never guesses silently.
    Parse(r io.Reader) ([]RawTrade, []ParseIssue, error)
}
```

- Registry auto-detects the broker; if the best confidence is below a threshold, ask the user to pick.
- Every rejected row becomes a `ParseIssue` with line number and reason. No silent drops.
- Each parser ships with sample files and golden tests. Adding a broker must not require touching the core (documented in `docs/adding-a-broker.md`).

---

## 9. API (REST, JSON, versioned under `/api/v1`)

Write `backend/api/openapi.yaml` first, then implement. All errors use `application/problem+json`. All list endpoints use cursor pagination. `POST` endpoints accept an `Idempotency-Key` header (stored in Redis for 24h).

| Method | Path | Purpose |
|---|---|---|
| POST | `/auth/register`, `/auth/login`, `/auth/refresh`, `/auth/logout` | Auth |
| GET | `/me` | Current user |
| DELETE | `/me` | Delete all of the user's data (files, trades, reports) |
| GET/POST | `/accounts` | List / create demat accounts |
| POST | `/uploads` | Multipart upload (file + account_id). Returns the existing upload if the sha256 matches (idempotent) |
| GET | `/jobs/{id}` | Job status |
| GET | `/jobs/{id}/events` | SSE progress stream |
| GET | `/review-items?status=open` | Items needing user input |
| POST | `/review-items/{id}/resolve` | Resolve one (e.g. enter missing buy price) → triggers recompute |
| GET | `/summary?fy=2025-26` | Totals per category, turnover, counts |
| GET | `/gains?fy=&category=&account=&isin=&cursor=` | Gain lines |
| GET | `/gains/{id}/explain` | "Why this number" tree |
| GET | `/trades?fy=&account=&isin=&cursor=` | Normalised trades |
| POST | `/reports` | `{fy, format: xlsx|pdf|json}` → job |
| GET | `/reports/{id}/download` | File download |
| GET | `/healthz`, `/readyz` | Liveness / readiness (DB + Redis reachable) |

**Explain response shape (example):**

```json
{
  "gain_id": "…",
  "category": "LTCG_EQUITY",
  "gain": "18450.0000",
  "sale": { "date": "2025-08-12", "quantity": "50", "price": "1520.00", "value": "76000.00" },
  "lots": [
    {
      "lot_id": "…",
      "account": "Zerodha",
      "acquired": "2017-11-03",
      "quantity": "50",
      "actual_cost": "52000.00",
      "grandfathering": { "fmv_31_jan_2018": "1150.00", "applied": true, "chosen_cost": "57550.00" },
      "holding_days": 2839,
      "adjustments": [ { "kind": "SPLIT", "ex_date": "2019-09-20", "ratio": "1:2" } ]
    }
  ],
  "rules_version": "fy2025-26@1",
  "steps": [ "Matched 50 shares FIFO in account Zerodha", "Held > 12 months → long-term", "Grandfathered cost used (higher than actual)" ]
}
```

---

## 10. Phases and Definition of Done

> Each phase ends with: all tests green (`make check`), README updated, `docs/` updated, and a short summary to the owner of what was built and what to test manually.

### Phase 0 — Project setup (½ day)
- Repo structure from Section 4, `docker-compose.yml`, `.env.example`, Makefile, `.gitignore` (must include `.env`, `backend/testdata/private/`, `.data/`, `node_modules/`, `dist/`).
- Git hook + `.claude/settings.json` from Section 15.
- `make up` starts Postgres + Redis; `make migrate` runs an empty first migration.
- **Done when:** `make up && make migrate && make check` works on a clean clone.

### Phase 1 — Core engine as a CLI (weeks 1–2)
- `domain`, `parser/zerodha`, `normalize`, `matcher`, `tax` (STCG/LTCG only), `report/json`.
- CLI: `taxpl calculate --fy 2025-26 file1.csv [file2.csv…]` prints a summary table and writes `out.json`.
- `cmd/seed` generates realistic synthetic Zerodha tradebooks.
- At least **8 golden cases**: simple buy/sell; partial sell across 2 lots; multiple ISINs; boundary at 12 months; sell with no buy (→ review item); two accounts holding the same ISIN (FIFO must stay per account); same-day buy+sell (→ intraday); FY boundary on 31 March / 1 April.
- Property tests (rapid): total matched quantity ≤ total bought; lot quantities never negative; sum of gains = sum of (sale value − cost) for matched quantities; result is independent of input file order.
- **Done when:** all golden + property tests pass and the owner has hand-checked at least one real tradebook.

### Phase 2 — API, database, background jobs (week 3)
- Migrations + sqlc queries for all core tables; auth; accounts; uploads (idempotent by sha256); River pipeline running the Phase 1 pure functions; SSE progress; review items API.
- Parsers for **Groww** and **Upstox** (+ Angel One if time allows), each with golden tests.
- Integration tests with testcontainers-go against real Postgres.
- **Done when:** uploading the same file twice yields one upload and identical results; killing the worker mid-job and restarting completes the job correctly.

### Phase 3 — Usable product + frontend (week 4)
- Frontend screens in Section 12: Landing, Auth, Dashboard, Upload with live progress, Review, Gains table, **Explain drawer**, Reports, Settings (delete my data).
- Excel and PDF reports; JSON export.
- Deterministic replay test: run the same inputs twice → identical report checksum.
- Playwright e2e: register → upload 2 broker files → resolve a review item → download Excel.
- **Done when:** a non-technical friend can upload their files and understand the result without help. Owner starts sharing with real users.

### Phase 4 — Hard edge cases + safety (weeks 5–6)
- Grandfathering (including the post-2018 split FMV adjustment), splits, bonus, demergers, merger/symbol change, buybacks (both regimes, split at 1 Apr 2026), off-market transfers, F&O P&L + turnover, charges (imported + estimated with date-based STT), intraday netting, all fully tested.
- The FY 2024-25 report splits equity gains before and after 23 July 2024.
- **Loss summary** (6.10): set-off view and carry-forward amounts per category, with the "file on time" warning.
- Rules files for FY 2024-25, FY 2025-26 and Tax Year 2026-27 (6.9), with the UI showing each year's law and labels.
- Per-user advisory lock + concurrency test (Section 8).
- Row-level security; audit log grants; raw-file purge job (`RAW_FILE_TTL_HOURS`); PAN stored only as a hash.
- **Holdings reconciliation:** user can upload the broker's current holdings file; the engine compares it with computed open lots and flags differences. (This catches missing history and is a strong interview story.)
- `docs/edge-cases.md` written with real examples.
- **Done when:** every edge case has a golden test and appears in `docs/edge-cases.md`.

### Phase 5 — Scale and polish (weeks 7–8)
- Benchmark: `make bench` generates 100k / 1M synthetic trades and records stage timings in `docs/benchmarks.md`. Optimise with profiling (`pprof`), batch inserts (`pgx.CopyFrom`), and fewer round trips.
- Optional: move stage-to-stage events to Kafka using a **transactional outbox** table and idempotent consumers; River stays for scheduled jobs. Write an ADR explaining the trade-off honestly (Kafka vs Postgres queue).
- Final README polish, demo GIF, architecture diagram, write-up.
- **Done when:** README shows benchmark numbers and a 2-minute demo flow works from a fresh clone.

---

## 11. Engineering standards (apply in every phase)

- **Tests first for domain code.** Write the golden test, watch it fail, then implement.
- **No floats for money or quantity, anywhere.** Add a lint rule or test that greps for `float64` in `internal/domain|matcher|tax|charges` and fails.
- **Errors:** wrap with context (`fmt.Errorf("parse row %d: %w", n, err)`); map to problem+json in one place.
- **Context everywhere:** every DB/network call takes `context.Context`; set timeouts.
- **Transactions:** a stage's writes + next job enqueue happen in one transaction.
- **Logging:** structured `slog` with `request_id`, `user_id`, `job_id`. Never log file contents, PAN, emails or tokens.
- **Security:** bcrypt/argon2id passwords; JWT 15-minute access token; refresh token httpOnly + SameSite=Lax; CORS only for `FRONTEND_ORIGIN`; upload size limit (e.g. 20 MB) and content sniffing; rate limits per user and per IP via Redis.
- **Commits:** small, focused, Conventional Commits (`feat(matcher): …`, `fix(parser/groww): …`, `test(tax): …`).
- **ADRs:** write `docs/adr/NNNN-title.md` for every significant decision (River vs Kafka, sqlc vs ORM, FIFO per account, local vs S3 blobs).

---

## 12. Frontend — design and build instructions

### 12.1 Use the design skills (mandatory)
Before building any UI, load and follow the installed design skills:

1. **`frontend-design`** skill (Anthropic's official one): use it to choose a distinctive visual direction and avoid generic, template-looking UI. Install it from Claude Code's plugin marketplace with `/plugin` if it is not already available.
2. **Impeccable** design skills/commands, if installed: use them to audit and polish typography, spacing, color, layout and interaction quality, especially before finishing Phase 3 and Phase 5.
3. **Motion** (Framer Motion) for animation, following Section 12.4.
4. Any other installed UI/UX, accessibility or animation skills: list them at the start of frontend work and say which ones you will use.

If a skill is not installed, say so and continue by following this section.

### 12.2 Design direction
- **Feel:** a precise, calm financial instrument, like a well-made ledger. Trustworthy, not flashy. The user is anxious about taxes; the UI should lower that anxiety.
- **Typography:** one clean sans for UI and a monospaced or tabular-figure font for numbers. **All numbers use tabular figures** so columns align.
- **Numbers:** format INR with `Intl.NumberFormat('en-IN', { style: 'currency', currency: 'INR' })` (lakh/crore grouping). Gains in a calm green, losses in a calm red, **always with a + / − sign as well**, so meaning never depends on color alone.
- **Color:** light and dark themes from the start, defined as CSS variables. One accent color. No purple gradients, no glassmorphism, no emoji in the UI.
- **Density:** tables are the core of the product; make them excellent (sticky headers, right-aligned numbers, sortable, filterable, keyboard navigable, virtualised for large data).
- **Trust signals:** show "Rules version FY 2025-26 v1", "Estimated" badges on estimated charges, and the not-tax-advice disclaimer in the footer and on reports.

### 12.3 Screens
1. **Landing** — one clear sentence on the problem, a 3-step "how it works", supported brokers, privacy promise (files deleted after 24 h).
2. **Sign up / Log in.**
3. **Dashboard** — year selector showing "FY 2025-26" / "Tax Year 2026-27" labels from the rules files; summary cards (STCG, LTCG, Intraday, F&O, F&O turnover); breakdown by account; loss carry-forward card (Phase 4); open review items callout; recent uploads.
4. **Upload** — drag-and-drop multiple files; broker auto-detected with an override; per-file live pipeline progress (SSE) showing each stage; clear error states with the row number for parse issues.
5. **Review** — one card per issue with a simple form to resolve it (e.g. enter a missing buy date and price); resolving triggers recompute with visible progress.
6. **Gains** — virtualised table filtered by category/account/ISIN; clicking a row opens the **Explain drawer**.
7. **Explain drawer** — a step-by-step visual: sale → matched lots (with account, dates, quantities) → adjustments (split/bonus/demerger) → grandfathering comparison → holding period → category → gain. This is the signature feature; make it beautiful and clear.
8. **Trades** — normalised trades explorer.
9. **Reports** — generate and download Excel/PDF/JSON per FY; show rules version and checksum.
10. **Settings** — accounts, change password, **delete all my data** (with confirmation).

### 12.4 Motion rules
- Use Motion for meaning, not decoration: stage-progress transitions, the Explain drawer opening, number count-ups on the dashboard (short), list reordering, and layout changes when filters apply.
- Durations 150–300 ms; spring or ease-out curves; no bouncing on financial numbers.
- **Respect `prefers-reduced-motion`**: wrap the app in `MotionConfig reducedMotion="user"` and avoid motion that conveys essential information.
- Keep motion presets in `src/lib/motion.ts` so the feel is consistent.

### 12.5 Quality bar
- Accessibility: semantic HTML, every input labelled, full keyboard support, visible focus, WCAG AA contrast, `aria-live` for upload progress.
- Responsive down to 375 px; tables scroll horizontally inside their container on small screens.
- Loading skeletons, empty states with a helpful next action, and error states with retry.
- Lighthouse performance and accessibility ≥ 90 on Dashboard and Gains.

---

## 13. Security and privacy (non-negotiable)

- Raw uploaded files are deleted after `RAW_FILE_TTL_HOURS` (default 24) by a scheduled River job; only parsed trades are kept.
- PAN is never stored in plain text (hash only, and only if the user enters it).
- `DELETE /me` removes everything for that user, including blobs and reports, and is covered by a test.
- Never commit real tradebooks. Real files for testing go only in `backend/testdata/private/` (gitignored). Golden files must be synthetic or fully anonymised.
- Secrets only from environment variables; `.env` is gitignored.

---

## 14. README requirements

The README is what recruiters read. It must have, in this order:

1. One-line description and a demo GIF or screenshots.
2. **The problem** (3–4 sentences, plain English).
3. **What it does** (bullet list) and supported brokers.
4. **Architecture** diagram + 1 paragraph; link to `docs/architecture.md`.
5. **Hard problems solved**: FIFO per account, grandfathering, corporate actions, intraday netting, idempotency, deterministic replay, concurrency. Link to `docs/edge-cases.md`.
6. **How correctness is proven**: golden tests, property tests, replay test, reconciliation.
7. **Benchmarks** table.
8. **Run locally** in ≤ 5 commands (`cp .env.example .env`, `make up`, `make migrate`, `make api` + `make worker`, `make web`).
9. Tech stack.
10. Disclaimer (not tax advice).
11. Author: **Shreyas Wani** with links (GitHub, LinkedIn).

---

## 15. Git, authorship and attribution (mandatory)

This is the owner's personal portfolio project. **Commits and pull requests must show only the owner as author.**

- Never change `git config user.name` or `user.email`.
- **Never** add `Co-Authored-By:` lines, "Generated with …" lines, session links, or any AI tool name to commit messages, PR titles/descriptions, code comments, the README or docs.
- Configure Claude Code so it doesn't add attribution. Create `.claude/settings.json`:
  ```json
  {
    "attribution": {
      "commit": "",
      "pr": ""
    }
  }
  ```
  (An empty string hides the attribution. If your Claude Code version is older, the deprecated equivalent is `"includeCoAuthoredBy": false`. Check with `/status` that the file loaded.)
- Safety net: a git hook that strips such lines even if one slips through. Create `.githooks/commit-msg`:
  ```sh
  #!/bin/sh
  # Remove AI attribution lines from commit messages.
  sed -i.bak -E \
    -e '/^[Cc]o-[Aa]uthored-[Bb]y:.*([Cc]laude|[Aa]nthropic|noreply@anthropic\.com)/d' \
    -e '/[Gg]enerated with .*[Cc]laude/d' \
    -e '/^[Cc]laude-[Ss]ession:/d' \
    -e '/claude\.ai\/code/d' \
    "$1" && rm -f "$1.bak"
  ```
  Make it executable and enable it: `chmod +x .githooks/commit-msg && git config core.hooksPath .githooks` (the `make hooks` target does this).
- GitHub's contributor list comes from commit authors and co-author trailers, so with the steps above only the owner appears.

---

## 16. Final checklist before calling the project done

- [ ] `make check` passes on a fresh clone with only Docker Desktop, Go and Node installed
- [ ] ≥ 25 golden cases covering every rule in Section 6
- [ ] Property tests for the matcher and holding-period boundary
- [ ] Idempotency test (same file twice), replay test (same checksum), concurrency test (parallel uploads)
- [ ] Zerodha, Groww, Upstox parsers (+ Angel One) with sample files
- [ ] Explain drawer works for every gain line
- [ ] Holdings reconciliation flags real differences
- [ ] Excel + PDF reports include rules version, checksum and disclaimer
- [ ] Raw-file purge and "delete my data" both tested
- [ ] Frontend passes the quality bar in 12.5, light and dark
- [ ] README complete (Section 14), docs/ complete, ADRs written
- [ ] Benchmarks recorded
- [ ] Every [ASK CA] item in 6.12 answered by the owner's CA, its golden test added, and `verified_by` filled in each rules file
- [ ] `docs/tax-rules.md` lists every rule with its source link (6.11) and verification date
- [ ] No AI attribution anywhere in git history (`git log --format=%B | grep -i -E "claude|anthropic"` returns nothing)
