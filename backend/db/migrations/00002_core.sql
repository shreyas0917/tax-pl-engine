-- +goose Up
-- +goose StatementBegin

-- Reference data (shared, not user-owned) -------------------------------------------------
CREATE TABLE ref.instruments (
    isin            TEXT PRIMARY KEY,
    symbol          TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    exchange        TEXT NOT NULL DEFAULT '',
    segment         TEXT NOT NULL DEFAULT 'EQ',
    fmv_2018_01_31  NUMERIC(20,4),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX instruments_symbol_idx ON ref.instruments (symbol);

CREATE TABLE ref.corporate_actions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    isin            TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('SPLIT','BONUS','DEMERGER','MERGER')),
    ratio_from      NUMERIC(20,6) NOT NULL,
    ratio_to        NUMERIC(20,6) NOT NULL,
    cost_split_pct  NUMERIC(10,6),
    new_isin        TEXT,
    new_symbol      TEXT,
    ex_date         DATE NOT NULL,
    source          TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX corporate_actions_isin_idx ON ref.corporate_actions (isin, ex_date);

-- Users and accounts ------------------------------------------------------------------------
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    pan_hash      TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);

CREATE TABLE demat_accounts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    broker      TEXT NOT NULL,
    nickname    TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, broker, nickname)
);
CREATE INDEX demat_accounts_user_idx ON demat_accounts (user_id);

-- Uploads and jobs --------------------------------------------------------------------------
CREATE TABLE uploads (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id  UUID NOT NULL REFERENCES demat_accounts(id) ON DELETE CASCADE,
    broker      TEXT NOT NULL DEFAULT '',
    filename    TEXT NOT NULL,
    sha256      TEXT NOT NULL,
    size_bytes  BIGINT NOT NULL,
    blob_key    TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'UPLOADED',
    purged_at   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, sha256)
);
CREATE INDEX uploads_user_idx ON uploads (user_id, created_at DESC);

CREATE TABLE jobs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    upload_id     UUID REFERENCES uploads(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL,   -- PROCESS_UPLOAD | RECOMPUTE | REPORT
    state         TEXT NOT NULL DEFAULT 'UPLOADED',
    rules_version TEXT NOT NULL DEFAULT '',
    attempt       INT NOT NULL DEFAULT 0,
    error         JSONB,
    payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX jobs_user_idx ON jobs (user_id, created_at DESC);
CREATE INDEX jobs_upload_idx ON jobs (upload_id);

CREATE TABLE job_events (
    id          BIGSERIAL PRIMARY KEY,
    job_id      UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL,
    state       TEXT NOT NULL,
    message     TEXT NOT NULL DEFAULT '',
    detail      JSONB,
    at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX job_events_job_idx ON job_events (job_id, id);

-- Trades (normalised input) ------------------------------------------------------------------
CREATE TABLE trades (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id      UUID NOT NULL REFERENCES demat_accounts(id) ON DELETE CASCADE,
    upload_id       UUID REFERENCES uploads(id) ON DELETE SET NULL,
    source_row_hash TEXT NOT NULL,
    isin            TEXT NOT NULL DEFAULT '',
    symbol          TEXT NOT NULL,
    segment         TEXT NOT NULL,
    kind            TEXT NOT NULL DEFAULT 'REGULAR',
    side            TEXT NOT NULL CHECK (side IN ('BUY','SELL')),
    quantity        NUMERIC(20,6) NOT NULL,
    price           NUMERIC(20,4) NOT NULL,
    trade_date      DATE NOT NULL,
    trade_time      TEXT NOT NULL DEFAULT '',
    exchange        TEXT NOT NULL DEFAULT '',
    order_id        TEXT NOT NULL DEFAULT '',
    trade_ref       TEXT NOT NULL DEFAULT '',
    expiry          DATE,
    strike          NUMERIC(20,4),
    option_type     TEXT NOT NULL DEFAULT '',
    original_acquisition_date DATE,
    original_cost   NUMERIC(20,4),
    charges         JSONB,
    raw             JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, source_row_hash)
);
CREATE INDEX trades_user_date_idx ON trades (user_id, trade_date);
CREATE INDEX trades_account_isin_idx ON trades (account_id, isin, trade_date);

-- User-supplied data that feeds matching -----------------------------------------------------
CREATE TABLE opening_lots (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id       UUID NOT NULL REFERENCES demat_accounts(id) ON DELETE CASCADE,
    isin             TEXT NOT NULL,
    symbol           TEXT NOT NULL DEFAULT '',
    quantity         NUMERIC(20,6) NOT NULL,
    cost_total       NUMERIC(20,4) NOT NULL,
    acquisition_date DATE NOT NULL,
    note             TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX opening_lots_user_idx ON opening_lots (user_id);

CREATE TABLE user_instrument_overrides (
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    isin            TEXT NOT NULL,
    symbol          TEXT NOT NULL DEFAULT '',
    fmv_2018_01_31  NUMERIC(20,4),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, isin)
);

-- Derived tables: always rebuilt by RECOMPUTE, never edited by hand -----------------------------
CREATE TABLE computations (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_id         UUID REFERENCES jobs(id) ON DELETE SET NULL,
    rules_versions JSONB NOT NULL,
    checksum       TEXT NOT NULL,
    trade_count    INT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX computations_user_idx ON computations (user_id, created_at DESC);

CREATE TABLE lots (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id        UUID NOT NULL REFERENCES demat_accounts(id) ON DELETE CASCADE,
    lot_key           TEXT NOT NULL,          -- engine's deterministic lot ID
    isin              TEXT NOT NULL,
    symbol            TEXT NOT NULL DEFAULT '',
    open_trade_key    TEXT NOT NULL DEFAULT '',
    origin            TEXT NOT NULL,
    quantity_original NUMERIC(20,6) NOT NULL,
    quantity_open     NUMERIC(20,6) NOT NULL,
    cost_original     NUMERIC(20,4) NOT NULL,
    cost_open         NUMERIC(20,4) NOT NULL,
    acquisition_date  DATE NOT NULL,
    grandfathered_fmv NUMERIC(20,4),
    adjustments       JSONB,
    UNIQUE (user_id, lot_key)
);
CREATE INDEX lots_user_account_idx ON lots (user_id, account_id, isin);

CREATE TABLE gains (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id          UUID NOT NULL REFERENCES demat_accounts(id) ON DELETE CASCADE,
    gain_key            TEXT NOT NULL,          -- engine's deterministic gain ID
    fy                  TEXT NOT NULL,
    category            TEXT NOT NULL,
    isin                TEXT NOT NULL DEFAULT '',
    symbol              TEXT NOT NULL DEFAULT '',
    contract_key        TEXT NOT NULL DEFAULT '',
    sale_date           DATE NOT NULL,
    acquisition_date    DATE,
    quantity            NUMERIC(20,6) NOT NULL,
    sale_value          NUMERIC(20,4) NOT NULL,
    actual_cost         NUMERIC(20,4) NOT NULL,
    cost_of_acquisition NUMERIC(20,4) NOT NULL,
    charges_deductible  NUMERIC(20,4) NOT NULL,
    gain                NUMERIC(20,4) NOT NULL,
    holding_days        INT NOT NULL DEFAULT 0,
    rate_period         TEXT NOT NULL DEFAULT '',
    buyback_regime      TEXT NOT NULL DEFAULT '',
    buyback_proceeds    NUMERIC(20,4) NOT NULL DEFAULT 0,
    rules_version       TEXT NOT NULL,
    explain             JSONB NOT NULL,         -- full domain.Gain + matched lots for "why this number"
    UNIQUE (user_id, gain_key)
);
CREATE INDEX gains_user_fy_idx ON gains (user_id, fy, category, sale_date);
CREATE INDEX gains_user_account_idx ON gains (user_id, account_id);

CREATE TABLE review_items (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_id      UUID REFERENCES jobs(id) ON DELETE SET NULL,
    review_key  TEXT NOT NULL,                  -- engine's deterministic review ID
    kind        TEXT NOT NULL,
    account_id  UUID REFERENCES demat_accounts(id) ON DELETE CASCADE,
    isin        TEXT NOT NULL DEFAULT '',
    symbol      TEXT NOT NULL DEFAULT '',
    trade_key   TEXT NOT NULL DEFAULT '',
    message     TEXT NOT NULL,
    payload     JSONB,
    status      TEXT NOT NULL DEFAULT 'open',   -- open | resolved | dismissed
    resolution  JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    UNIQUE (user_id, review_key)
);
CREATE INDEX review_items_user_status_idx ON review_items (user_id, status);

CREATE TABLE reports (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_id        UUID REFERENCES jobs(id) ON DELETE SET NULL,
    fy            TEXT NOT NULL,
    format        TEXT NOT NULL CHECK (format IN ('xlsx','pdf','json')),
    blob_key      TEXT NOT NULL,
    checksum      TEXT NOT NULL,
    rules_version TEXT NOT NULL,
    size_bytes    BIGINT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX reports_user_idx ON reports (user_id, created_at DESC);

-- Append-only audit log --------------------------------------------------------------------
CREATE TABLE audit_log (
    id         BIGSERIAL PRIMARY KEY,
    user_id    UUID,
    entity     TEXT NOT NULL,
    entity_id  TEXT NOT NULL,
    action     TEXT NOT NULL,
    before     JSONB,
    after      JSONB,
    at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_user_idx ON audit_log (user_id, at DESC);

-- Idempotency keys for POST endpoints are in Redis; this table backs them when Redis is absent.
CREATE TABLE idempotency_keys (
    user_id    UUID NOT NULL,
    key        TEXT NOT NULL,
    response   JSONB NOT NULL,
    status     INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS idempotency_keys, audit_log, reports, review_items, gains, lots, computations,
    user_instrument_overrides, opening_lots, trades, job_events, jobs, uploads, demat_accounts,
    refresh_tokens, users CASCADE;
DROP TABLE IF EXISTS ref.corporate_actions, ref.instruments CASCADE;
-- +goose StatementEnd
