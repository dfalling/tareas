-- Named queries for API tokens, compiled to typed pgx/v5 code by sqlc (see
-- sqlc.yaml at the repo root). Only sha256(token) is ever stored.

-- name: CreateToken :one
INSERT INTO api_tokens (user_id, token_hash, token_prefix, name, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, user_id, token_hash, token_prefix, name, last_used_at, expires_at, created_at;

-- The auth lookup is by token_hash only: the plaintext token never reaches
-- SQL, and the row's user_id scopes everything that follows.
-- name: TokenByHash :one
SELECT id, user_id, token_hash, token_prefix, name, last_used_at, expires_at, created_at
FROM api_tokens
WHERE token_hash = $1;

-- Newest first for the settings page list.
-- name: ListTokensByUser :many
SELECT id, user_id, token_hash, token_prefix, name, last_used_at, expires_at, created_at
FROM api_tokens
WHERE user_id = $1
ORDER BY created_at DESC, id DESC;

-- Best-effort metadata write after a successful authenticated API request.
-- name: TouchTokenLastUsed :exec
UPDATE api_tokens
SET last_used_at = now()
WHERE id = $1;

-- RowsAffected reports whether the token belonged to this user at all
-- (0 = not found).
-- name: RevokeToken :execrows
DELETE FROM api_tokens
WHERE id = $1 AND user_id = $2;

-- NULL expires_at never satisfies the comparison, so non-expiring tokens are
-- naturally excluded.
-- name: PurgeExpiredTokens :execrows
DELETE FROM api_tokens
WHERE expires_at < now();
