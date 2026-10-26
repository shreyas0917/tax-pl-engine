-- name: CreateUser :one
INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: UpdatePassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: SetPANHash :exec
UPDATE users SET pan_hash = $2 WHERE id = $1;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3) RETURNING *;

-- name: GetRefreshToken :one
SELECT * FROM refresh_tokens WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now();

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1;

-- name: RevokeAllRefreshTokens :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- name: CreateAccount :one
INSERT INTO demat_accounts (user_id, broker, nickname) VALUES ($1, $2, $3) RETURNING *;

-- name: ListAccounts :many
SELECT * FROM demat_accounts WHERE user_id = $1 ORDER BY created_at, id;

-- name: GetAccount :one
SELECT * FROM demat_accounts WHERE id = $1 AND user_id = $2;

-- name: DeleteAccount :exec
DELETE FROM demat_accounts WHERE id = $1 AND user_id = $2;
