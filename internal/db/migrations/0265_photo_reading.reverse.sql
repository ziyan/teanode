DROP INDEX IF EXISTS "agent_attachment_photo";
ALTER TABLE "agent_attachment" DROP COLUMN IF EXISTS "photo_id";
DROP INDEX IF EXISTS "photo_reading_waiting";
ALTER TABLE "photo" DROP COLUMN IF EXISTS "reading_queued_at", DROP COLUMN IF EXISTS "photo_document_kind";
ALTER TABLE "user" DROP COLUMN IF EXISTS "is_photo_reading_enabled";
