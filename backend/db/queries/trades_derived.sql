-- name: InsertTrade :execrows
INSERT INTO trades (user_id, account_id, upload_id, source_row_hash, isin, symbol, segment, kind, side, quantity, price,
  trade_date, trade_time, exchange, order_id, trade_ref, expiry, strike, option_type, original_acquisition_date, original_cost, charges, raw)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
ON CONFLICT (user_id, source_row_hash) DO NOTHING;

-- name: ListTradesForUser :many
SELECT * FROM trades WHERE user_id = $1 ORDER BY trade_date, trade_time, trade_ref, source_row_hash;

-- name: ListTradesPage :many
SELECT t.*, a.nickname AS account_name, a.broker AS account_broker FROM trades t
JOIN demat_accounts a ON a.id = t.account_id
WHERE t.user_id = $1
  AND (sqlc.narg('fy_start')::date IS NULL OR t.trade_date >= sqlc.narg('fy_start')::date)
  AND (sqlc.narg('fy_end')::date IS NULL OR t.trade_date <= sqlc.narg('fy_end')::date)
  AND (sqlc.narg('account_id')::uuid IS NULL OR t.account_id = sqlc.narg('account_id')::uuid)
  AND (sqlc.narg('isin')::text IS NULL OR t.isin = sqlc.narg('isin')::text)
  AND (t.trade_date, t.id::text) > (sqlc.arg('cursor_date')::date, sqlc.arg('cursor_id')::text)
ORDER BY t.trade_date, t.id::text
LIMIT sqlc.arg('page_size');

-- name: CountTrades :one
SELECT count(*) FROM trades WHERE user_id = $1;

-- name: DeleteTradesForUpload :execrows
DELETE FROM trades WHERE upload_id = $1 AND user_id = $2;

-- name: ListOpeningLots :many
SELECT * FROM opening_lots WHERE user_id = $1 ORDER BY acquisition_date, id;

-- name: InsertOpeningLot :one
INSERT INTO opening_lots (user_id, account_id, isin, symbol, quantity, cost_total, acquisition_date, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: ListInstrumentOverrides :many
SELECT * FROM user_instrument_overrides WHERE user_id = $1;

-- name: UpsertInstrumentOverride :exec
INSERT INTO user_instrument_overrides (user_id, isin, symbol, fmv_2018_01_31) VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, isin) DO UPDATE SET symbol = COALESCE(NULLIF(EXCLUDED.symbol, ''), user_instrument_overrides.symbol),
  fmv_2018_01_31 = COALESCE(EXCLUDED.fmv_2018_01_31, user_instrument_overrides.fmv_2018_01_31);

-- name: ListInstruments :many
SELECT * FROM ref.instruments;

-- name: GetInstrumentsByISIN :many
SELECT * FROM ref.instruments WHERE isin = ANY($1::text[]);

-- name: UpsertInstrument :exec
INSERT INTO ref.instruments (isin, symbol, name, exchange, segment, fmv_2018_01_31) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (isin) DO UPDATE SET symbol = EXCLUDED.symbol, name = EXCLUDED.name, exchange = EXCLUDED.exchange,
  segment = EXCLUDED.segment, fmv_2018_01_31 = COALESCE(EXCLUDED.fmv_2018_01_31, ref.instruments.fmv_2018_01_31);

-- name: ListCorporateActions :many
SELECT * FROM ref.corporate_actions ORDER BY ex_date, id;

-- name: InsertCorporateAction :one
INSERT INTO ref.corporate_actions (isin, kind, ratio_from, ratio_to, cost_split_pct, new_isin, new_symbol, ex_date, source)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING *;

-- name: DeleteDerivedForUser :exec
DELETE FROM gains WHERE user_id = $1;

-- name: DeleteLotsForUser :exec
DELETE FROM lots WHERE user_id = $1;

-- name: InsertLot :exec
INSERT INTO lots (user_id, account_id, lot_key, isin, symbol, open_trade_key, origin, quantity_original, quantity_open,
  cost_original, cost_open, acquisition_date, grandfathered_fmv, adjustments)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- name: ListOpenLots :many
SELECT l.*, a.nickname AS account_name FROM lots l JOIN demat_accounts a ON a.id = l.account_id
WHERE l.user_id = $1 AND l.quantity_open > 0 ORDER BY a.nickname, l.isin, l.acquisition_date;

-- name: InsertGain :exec
INSERT INTO gains (user_id, account_id, gain_key, fy, category, isin, symbol, contract_key, sale_date, acquisition_date,
  quantity, sale_value, actual_cost, cost_of_acquisition, charges_deductible, gain, holding_days, rate_period,
  buyback_regime, buyback_proceeds, rules_version, explain)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22);

-- name: ListGainsForFY :many
SELECT g.*, a.nickname AS account_name FROM gains g JOIN demat_accounts a ON a.id = g.account_id
WHERE g.user_id = $1 AND g.fy = $2 ORDER BY g.sale_date, a.nickname, g.symbol, g.acquisition_date, g.gain_key;

-- name: ListGainsPage :many
SELECT g.*, a.nickname AS account_name FROM gains g JOIN demat_accounts a ON a.id = g.account_id
WHERE g.user_id = $1 AND g.fy = $2
  AND (sqlc.narg('category')::text IS NULL OR g.category = sqlc.narg('category')::text)
  AND (sqlc.narg('account_id')::uuid IS NULL OR g.account_id = sqlc.narg('account_id')::uuid)
  AND (sqlc.narg('isin')::text IS NULL OR g.isin = sqlc.narg('isin')::text OR g.symbol = sqlc.narg('isin')::text)
  AND (g.sale_date, g.gain_key) > (sqlc.arg('cursor_date')::date, sqlc.arg('cursor_key')::text)
ORDER BY g.sale_date, g.gain_key
LIMIT sqlc.arg('page_size');

-- name: GetGain :one
SELECT g.*, a.nickname AS account_name FROM gains g JOIN demat_accounts a ON a.id = g.account_id
WHERE g.id = $1 AND g.user_id = $2;

-- name: ListGainFYs :many
SELECT DISTINCT fy FROM gains WHERE user_id = $1 ORDER BY fy;

-- name: InsertComputation :one
INSERT INTO computations (user_id, job_id, rules_versions, checksum, trade_count) VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: LatestComputation :one
SELECT * FROM computations WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: UpsertReviewItem :exec
INSERT INTO review_items (user_id, job_id, review_key, kind, account_id, isin, symbol, trade_key, message, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (user_id, review_key) DO UPDATE SET job_id = EXCLUDED.job_id, message = EXCLUDED.message, payload = EXCLUDED.payload;

-- name: CloseStaleReviewItems :execrows
UPDATE review_items SET status = 'resolved', resolved_at = now(), resolution = '{"auto":"no longer raised by the engine"}'::jsonb
WHERE user_id = $1 AND status = 'open' AND NOT (review_key = ANY($2::text[]));

-- name: ListReviewItems :many
SELECT r.*, a.nickname AS account_name FROM review_items r LEFT JOIN demat_accounts a ON a.id = r.account_id
WHERE r.user_id = $1 AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status')::text) ORDER BY r.created_at, r.id;

-- name: GetReviewItem :one
SELECT * FROM review_items WHERE id = $1 AND user_id = $2;

-- name: ResolveReviewItem :exec
UPDATE review_items SET status = $3, resolution = $4, resolved_at = now() WHERE id = $1 AND user_id = $2;

-- name: CountOpenReviewItems :one
SELECT count(*) FROM review_items WHERE user_id = $1 AND status = 'open';

-- name: CreateReport :one
INSERT INTO reports (user_id, job_id, fy, format, blob_key, checksum, rules_version, size_bytes) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: GetReport :one
SELECT * FROM reports WHERE id = $1 AND user_id = $2;

-- name: ListReports :many
SELECT * FROM reports WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: ListReportBlobKeys :many
SELECT id, blob_key FROM reports WHERE user_id = $1;

-- name: SetTradeOriginalByKey :execrows
UPDATE trades SET original_acquisition_date = $3, original_cost = $4 WHERE user_id = $1 AND source_row_hash = $2;

-- name: InsertManualTrade :execrows
INSERT INTO trades (user_id, account_id, source_row_hash, isin, symbol, segment, kind, side, quantity, price, trade_date, original_acquisition_date, original_cost, raw)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (user_id, source_row_hash) DO NOTHING;
