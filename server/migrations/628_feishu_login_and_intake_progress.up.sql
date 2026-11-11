CREATE TABLE IF NOT EXISTS feishu_identity (
    user_id UUID PRIMARY KEY,
    open_id TEXT NOT NULL,
    union_id TEXT,
    email TEXT NOT NULL,
    name TEXT NOT NULL,
    avatar_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE intake_request ADD COLUMN IF NOT EXISTS progress_done TEXT;
ALTER TABLE intake_request ADD COLUMN IF NOT EXISTS progress_remain TEXT;
