-- 002: ensure metadata_json exists on policy_bundles (safe to re-run).
-- The column is already in 001_init; this migration is kept for
-- databases created before the schema was consolidated.
ALTER TABLE policy_bundles ADD COLUMN metadata_json TEXT DEFAULT '{}';
