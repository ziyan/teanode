-- The source generation a recording's knowledge was last synced at. It is
-- null for a recording nothing has asked to sync since this column was added,
-- so adding it rewrites no row.
ALTER TABLE "recording" ADD COLUMN "knowledge_synced_generation" bigint;
