-- Remembered Feishu target for one person in one chat. No foreign keys.
CREATE TABLE IF NOT EXISTS org_bot_chat_context (
    installation_id uuid NOT NULL,
    chat_id text NOT NULL,
    user_id uuid NOT NULL,
    target_kind text NOT NULL DEFAULT '',
    unit_id uuid,
    unit_path text NOT NULL DEFAULT '',
    project_id uuid,
    project_name text NOT NULL DEFAULT '',
    awaiting text NOT NULL DEFAULT '',
    pending_text text NOT NULL DEFAULT '',
    pending_candidates text NOT NULL DEFAULT '[]',
    last_issue_id uuid,
    last_issue_identifier text NOT NULL DEFAULT '',
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT org_bot_chat_context_pkey PRIMARY KEY (installation_id, chat_id, user_id)
);
