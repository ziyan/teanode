-- A person's photos and videos: rows here, bytes in the blob store.
--
-- A photo is one item in the library: a picture, a video, or a Live Photo,
-- which is a picture with a short video. What it is made of is in
-- "photo_file", one row per file, so a Live Photo's video and an edited
-- version made on a phone sit beside the original instead of being separate
-- photos. The uploader gives each photo a stable name of its own
-- ("client_photo_id"; the phone's iCloud identifier when it has one, the
-- SHA-256 of the original from the dashboard and the command line), and
-- sending the same photo again finds the row instead of making a second.
--
-- Photos belong to an account and go with it. "taken_at" is null until the
-- file or the uploader says when; the timeline falls back to "created_at"
-- until then, which is why the index below is on that expression.
CREATE TABLE "photo" (
    "id" varchar(32) PRIMARY KEY,
    "user_id" varchar(32) NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
    "created_at" timestamptz NOT NULL,
    "updated_at" timestamptz NOT NULL,
    "client_photo_id" varchar(255) NOT NULL,
    "photo_kind" varchar(32) NOT NULL,
    "photo_source" varchar(32) NOT NULL,
    "taken_at" timestamptz,
    -- The camera's offset from UTC when it took the photo, so "that
    -- evening" can be told in the place it was and not in the viewer's zone.
    "taken_at_offset_minutes" integer,
    "width_pixels" integer NOT NULL DEFAULT 0,
    "height_pixels" integer NOT NULL DEFAULT 0,
    "duration_ms" bigint NOT NULL DEFAULT 0,
    "latitude" double precision,
    "longitude" double precision,
    "place_name" varchar(255) NOT NULL DEFAULT '',
    "camera_make" varchar(128) NOT NULL DEFAULT '',
    "camera_model" varchar(128) NOT NULL DEFAULT '',
    "original_filename" varchar(255) NOT NULL DEFAULT '',
    "is_favorite" boolean NOT NULL DEFAULT false,
    "is_screenshot" boolean NOT NULL DEFAULT false,
    -- Previews and metadata are made after the upload, by a periodic job
    -- that may run on any instance. "processing_claimed_at" is how two of
    -- them avoid taking the same photo, and a claim that old is taken again
    -- because the instance that made it is gone.
    "photo_processing_state" varchar(32) NOT NULL,
    "processing_attempt_count" integer NOT NULL DEFAULT 0,
    "processing_error_message" text NOT NULL DEFAULT '',
    -- How many failures in a row were not the file's (the store, the disk or a
    -- program the server lacks), which give the attempt back and so are not
    -- in "processing_attempt_count". It makes each wait longer than the last.
    "processing_outage_count" integer NOT NULL DEFAULT 0,
    "processing_claimed_at" timestamptz,
    -- A failed attempt waits before the next: "processing_not_before" is the
    -- earliest time a waiting photo is claimed again, null for no wait.
    "processing_not_before" timestamptz,
    -- A deleted photo waits here before it is deleted for good.
    "trashed_at" timestamptz,
    UNIQUE ("user_id", "client_photo_id")
);
CREATE INDEX "photo_user_timeline" ON "photo" ("user_id", (COALESCE("taken_at", "created_at")) DESC, "id" DESC) WHERE "trashed_at" IS NULL;
CREATE INDEX "photo_user_trashed" ON "photo" ("user_id", "trashed_at") WHERE "trashed_at" IS NOT NULL;
CREATE INDEX "photo_processing_waiting" ON "photo" ("photo_processing_state", "created_at") WHERE "photo_processing_state" = 'waiting';

-- Another name an uploader has for a photo that is here already. The same
-- picture can reach the library under two names: kept from a conversation
-- or a shared album (named by the digest of its bytes), and then backed up by
-- a phone under its own. The second upload is recorded against the photo
-- that has those bytes, and its name goes here so that the uploader finds the
-- photo by it, as it does by the name in "photo"."client_photo_id".
CREATE TABLE "photo_client_alias" (
    "user_id" varchar(32) NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
    "client_photo_id" varchar(255) NOT NULL,
    "photo_id" varchar(32) NOT NULL REFERENCES "photo"("id") ON DELETE CASCADE,
    "created_at" timestamptz NOT NULL,
    PRIMARY KEY ("user_id", "client_photo_id")
);
CREATE INDEX "photo_client_alias_photo" ON "photo_client_alias" ("photo_id");

-- The bytes of a file, named by their SHA-256, so the same picture saved
-- twice, or by two people, is stored once. The row exists only after the
-- bytes are in the store.
--
-- "deleting_at" is set before cleanup deletes the bytes. A file arriving
-- with the same digest in that window sees it and stores the bytes again
-- instead of trusting a row whose bytes may be half gone.
--
-- "unreferenced_since" is when no file named the blob any more (when the blob
-- was made, if none has yet), and null while a file does. It is what cleanup
-- reads to find blobs to delete, through the index below, so that finding them
-- is not a look at every blob there is. A trigger on "photo_file" keeps it, so
-- that a file going for any reason, such as its photo or its account being
-- deleted, is counted.
CREATE TABLE "photo_blob" (
    "blob_sha256" varchar(64) PRIMARY KEY,
    "blob_size_bytes" bigint NOT NULL CHECK ("blob_size_bytes" >= 0),
    "created_at" timestamptz NOT NULL,
    "deleting_at" timestamptz,
    "unreferenced_since" timestamptz
);
CREATE INDEX "photo_blob_unreferenced" ON "photo_blob" ("unreferenced_since") WHERE "unreferenced_since" IS NOT NULL;

-- One file of a photo, by role. Sending a role again replaces the file, which
-- is how an edit made on the phone later reaches the server; the blob it
-- pointed at is deleted by cleanup once nothing refers to it.
CREATE TABLE "photo_file" (
    "photo_id" varchar(32) NOT NULL REFERENCES "photo"("id") ON DELETE CASCADE,
    "photo_file_role" varchar(32) NOT NULL,
    "blob_sha256" varchar(64) NOT NULL REFERENCES "photo_blob"("blob_sha256"),
    "content_type" varchar(128) NOT NULL DEFAULT '',
    "file_size_bytes" bigint NOT NULL CHECK ("file_size_bytes" >= 0),
    "original_filename" varchar(255) NOT NULL DEFAULT '',
    "created_at" timestamptz NOT NULL,
    PRIMARY KEY ("photo_id", "photo_file_role")
);
CREATE INDEX "photo_file_blob" ON "photo_file" ("blob_sha256");

-- Keeps "photo_blob"."unreferenced_since": cleared when a file names the blob,
-- set when the last file that did stops.
CREATE FUNCTION "photo_file_track_blob_references"() RETURNS trigger AS $$
BEGIN
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        UPDATE "photo_blob" SET "unreferenced_since" = NULL
        WHERE "blob_sha256" = NEW."blob_sha256" AND "unreferenced_since" IS NOT NULL;
    END IF;
    IF TG_OP = 'DELETE' OR (TG_OP = 'UPDATE' AND OLD."blob_sha256" <> NEW."blob_sha256") THEN
        -- Two transactions taking away the last two files of one blob would
        -- each still see the other's file and neither would mark it. The lock
        -- makes the second wait for the first, and its next statement sees
        -- the first one's file gone.
        PERFORM 1 FROM "photo_blob" WHERE "blob_sha256" = OLD."blob_sha256" FOR UPDATE;
        UPDATE "photo_blob" SET "unreferenced_since" = now()
        WHERE "blob_sha256" = OLD."blob_sha256" AND "unreferenced_since" IS NULL
            AND NOT EXISTS (SELECT 1 FROM "photo_file" WHERE "blob_sha256" = OLD."blob_sha256");
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER "photo_file_blob_references" AFTER INSERT OR UPDATE OR DELETE ON "photo_file"
    FOR EACH ROW EXECUTE FUNCTION "photo_file_track_blob_references"();

-- A file on its way in, over the resumable upload protocol. Its parts are
-- stored one by one as they arrive, each with a receipt row below, so a
-- phone whose connection drops asks where it got to and sends the rest.
-- "upload_headers" is what the first request said the file is, kept because
-- the last request, which may come days later, is the one that completes it.
CREATE TABLE "photo_upload" (
    "id" varchar(32) PRIMARY KEY,
    "user_id" varchar(32) NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
    "created_at" timestamptz NOT NULL,
    "updated_at" timestamptz NOT NULL,
    "photo_upload_state" varchar(32) NOT NULL,
    "declared_size_bytes" bigint,
    "received_size_bytes" bigint NOT NULL DEFAULT 0 CHECK ("received_size_bytes" >= 0),
    -- How many parts have a receipt, which is the index of the next part, so
    -- that appending does not list or count them.
    "part_count" integer NOT NULL DEFAULT 0 CHECK ("part_count" >= 0),
    "upload_headers" jsonb NOT NULL,
    "photo_id" varchar(32)
);
CREATE INDEX "photo_upload_owner" ON "photo_upload" ("user_id", "created_at" DESC);
CREATE INDEX "photo_upload_idle" ON "photo_upload" ("updated_at");

-- A receipt for a part that is whole in the store. Only parts with a receipt
-- count towards the offset a client is told, so a part cut short by a
-- dropped connection is as if it had not been sent.
CREATE TABLE "photo_upload_part" (
    "photo_upload_id" varchar(32) NOT NULL REFERENCES "photo_upload"("id") ON DELETE CASCADE,
    "part_index" integer NOT NULL CHECK ("part_index" >= 0),
    "part_size_bytes" bigint NOT NULL CHECK ("part_size_bytes" > 0),
    "part_sha256" varchar(64) NOT NULL,
    "stored_at" timestamptz NOT NULL,
    PRIMARY KEY ("photo_upload_id", "part_index")
);

-- A smaller picture or video made from a photo's file for showing it. Derived
-- and never the only copy: they can be made again from the file.
CREATE TABLE "photo_preview" (
    "photo_id" varchar(32) NOT NULL REFERENCES "photo"("id") ON DELETE CASCADE,
    "photo_preview_size" varchar(32) NOT NULL,
    "content_type" varchar(128) NOT NULL,
    "preview_size_bytes" bigint NOT NULL CHECK ("preview_size_bytes" >= 0),
    "width_pixels" integer NOT NULL DEFAULT 0,
    "height_pixels" integer NOT NULL DEFAULT 0,
    "created_at" timestamptz NOT NULL,
    PRIMARY KEY ("photo_id", "photo_preview_size")
);

-- A phone backs up with a device password that grants the library and
-- nothing else, the way a calendar's does, so it keeps working when its
-- session would have expired.
ALTER TABLE "sync_credential" ADD COLUMN "has_photos_access" boolean NOT NULL DEFAULT false;

-- Keeping a photo library is something a member does with their own
-- account, like keeping a calendar. An operator can take it away from a role
-- in the usual place.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'photos:use' FROM "role" WHERE lower("name") IN ('administrator', 'operator', 'member')
ON CONFLICT DO NOTHING;
