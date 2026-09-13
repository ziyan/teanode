DROP INDEX "calendar_object_indexed_until";
CREATE INDEX "calendar_object_indexed_until" ON "calendar_object" ("indexed_until")
    WHERE "recurring";
ALTER TABLE "calendar_object" DROP COLUMN "indexed_at";
