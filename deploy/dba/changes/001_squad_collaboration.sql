-- Squad guardrails and org-sync provenance (migration 644).
--
-- wip_limit caps how many child issues under one squad-assigned parent may be
-- open at the same time; max_children caps the whole dispatch. Both are
-- enforced when the leader creates a child issue, so a runaway loop stops at
-- the server instead of only in the leader's prompt.
--
-- squad_member.source separates members synced from the org tree from members
-- a human added by hand. Re-syncing a department only prunes the former.
ALTER TABLE squad
    ADD COLUMN IF NOT EXISTS wip_limit int NOT NULL DEFAULT 3,
    ADD COLUMN IF NOT EXISTS max_children int NOT NULL DEFAULT 8;

ALTER TABLE squad_member
    ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'manual';

INSERT INTO schema_migrations (version) VALUES ('001_squad_collaboration');
