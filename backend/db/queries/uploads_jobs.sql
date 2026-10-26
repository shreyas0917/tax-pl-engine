-- name: CreateUpload :one
INSERT INTO uploads (user_id, account_id, broker, filename, sha256, size_bytes, blob_key, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'UPLOADED')
ON CONFLICT (user_id, sha256) DO NOTHING
RETURNING *;

-- name: GetUploadBySHA :one
SELECT * FROM uploads WHERE user_id = $1 AND sha256 = $2;

-- name: GetUpload :one
SELECT * FROM uploads WHERE id = $1 AND user_id = $2;

-- name: ListUploads :many
SELECT * FROM uploads WHERE user_id = $1 ORDER BY created_at DESC, id LIMIT $2;

-- name: SetUploadStatus :exec
UPDATE uploads SET status = $3, broker = COALESCE(NULLIF($4, ''), broker) WHERE id = $1 AND user_id = $2;

-- name: ListUploadsToPurge :many
SELECT * FROM uploads WHERE purged_at IS NULL AND created_at < $1 ORDER BY created_at LIMIT 500;

-- name: MarkUploadPurged :exec
UPDATE uploads SET purged_at = now() WHERE id = $1;

-- name: ListUploadBlobKeys :many
SELECT id, blob_key FROM uploads WHERE user_id = $1 AND purged_at IS NULL;

-- name: CreateJob :one
INSERT INTO jobs (user_id, upload_id, kind, state, payload) VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = $1 AND user_id = $2;

-- name: GetJobAny :one
SELECT * FROM jobs WHERE id = $1;

-- name: SetJobState :one
UPDATE jobs SET state = sqlc.arg('state')::text, updated_at = now(),
  started_at = COALESCE(started_at, CASE WHEN sqlc.arg('state')::text <> 'UPLOADED' THEN now() END),
  finished_at = CASE WHEN sqlc.arg('state')::text IN ('REPORT_READY','FAILED','NEEDS_REVIEW') THEN now() ELSE finished_at END,
  error = sqlc.narg('error')::jsonb,
  rules_version = COALESCE(NULLIF(sqlc.arg('rules_version')::text, ''), rules_version),
  attempt = attempt + sqlc.arg('attempt_inc')::int
WHERE id = sqlc.arg('id') RETURNING *;

-- name: ListJobsForUpload :many
SELECT * FROM jobs WHERE upload_id = $1 ORDER BY created_at;

-- name: ListRecentJobs :many
SELECT * FROM jobs WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: InsertJobEvent :one
INSERT INTO job_events (job_id, user_id, state, message, detail) VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: ListJobEvents :many
SELECT * FROM job_events WHERE job_id = $1 AND id > $2 ORDER BY id;

-- name: InsertAudit :exec
INSERT INTO audit_log (user_id, entity, entity_id, action, before, after) VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListAudit :many
SELECT * FROM audit_log WHERE user_id = $1 ORDER BY id DESC LIMIT $2;

-- name: PutIdempotencyKey :exec
INSERT INTO idempotency_keys (user_id, key, response, status) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING;

-- name: GetIdempotencyKey :one
SELECT * FROM idempotency_keys WHERE user_id = $1 AND key = $2 AND created_at > now() - interval '24 hours';

-- name: ListJobsInState :many
SELECT * FROM jobs WHERE user_id = $1 AND state = $2 ORDER BY created_at;

-- name: ListUsersWithJobsInState :many
SELECT DISTINCT user_id FROM jobs WHERE state = $1;
