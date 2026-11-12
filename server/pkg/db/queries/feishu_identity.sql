-- name: GetFeishuIdentityByOpenID :one
SELECT * FROM feishu_identity
WHERE open_id = $1;

-- name: GetFeishuIdentityByUserID :one
SELECT * FROM feishu_identity
WHERE user_id = $1;

-- name: SaveFeishuIdentity :exec
INSERT INTO feishu_identity (user_id, open_id, union_id, email, name, avatar_url, updated_at)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(open_id),
    sqlc.arg(union_id)::text,
    sqlc.arg(email),
    sqlc.arg(name),
    sqlc.arg(avatar_url)::text,
    now()
)
ON CONFLICT (user_id) DO UPDATE SET
    open_id = EXCLUDED.open_id,
    union_id = EXCLUDED.union_id,
    email = EXCLUDED.email,
    name = EXCLUDED.name,
    avatar_url = EXCLUDED.avatar_url,
    updated_at = now();

-- name: UpdateUserNameAndAvatar :one
UPDATE "user"
SET name = sqlc.arg(name), avatar_url = sqlc.arg(avatar_url), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;
