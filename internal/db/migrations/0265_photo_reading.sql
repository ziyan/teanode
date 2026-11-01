-- New photos are read as they arrive: described, and a receipt, a statement
-- or a tax document in one handed to the job that records it.
--
-- "is_photo_reading_enabled" is the person's own switch, on until they turn
-- it off. "photo_document_kind" is what the description said the picture is:
-- '' until it is described, then 'none', 'receipt', 'statement' or
-- 'taxDocument'. "reading_queued_at" is when the photo was queued to be read
-- because it was new, so that it is queued once.
ALTER TABLE "user" ADD COLUMN "is_photo_reading_enabled" boolean NOT NULL DEFAULT true;

ALTER TABLE "photo"
    ADD COLUMN "photo_document_kind" varchar(32) NOT NULL DEFAULT ''
        CHECK ("photo_document_kind" IN ('', 'none', 'receipt', 'statement', 'taxDocument')),
    ADD COLUMN "reading_queued_at" timestamptz;

-- The new photos still to be queued, found by the person and the day.
CREATE INDEX "photo_reading_waiting" ON "photo" ("user_id", (COALESCE("taken_at", "created_at")) DESC)
    WHERE "reading_queued_at" IS NULL AND "described_at" IS NULL AND "trashed_at" IS NULL;

-- The photo a document was copied from, so that one photo is handed to a
-- job once. Not a reference: the copy, and the receipt read from it, outlive
-- the photo.
ALTER TABLE "agent_attachment" ADD COLUMN "photo_id" varchar(32) NOT NULL DEFAULT '';
CREATE UNIQUE INDEX "agent_attachment_photo" ON "agent_attachment" ("agent_id", "photo_id") WHERE "photo_id" <> '';
