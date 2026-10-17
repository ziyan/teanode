-- What a model said is in a picture, kept on the photo, and what finds a
-- photo by words.
--
-- A photo is described only when somebody works with it (the agent looks at
-- it, or the person asks), so most photos have no description for a long
-- time and "photo_description" is empty until then. "described_at" is null
-- until a description was written; "description_requested_at" is set while a
-- description is wanted and the job that writes it has not finished.
-- "description_visible_text" is the words the model could read in the
-- picture, apart from its own sentences about it. "description_model" is the
-- model that wrote both.
ALTER TABLE "photo"
    ADD COLUMN "photo_description" text NOT NULL DEFAULT '',
    ADD COLUMN "description_visible_text" text NOT NULL DEFAULT '',
    ADD COLUMN "description_model" varchar(128) NOT NULL DEFAULT '',
    ADD COLUMN "described_at" timestamptz,
    ADD COLUMN "description_requested_at" timestamptz,
    ADD COLUMN "search_document" tsvector;

-- "search_document" is what a search by words reads. The code keeps it up to
-- date whenever the place, the file name, the camera, the description, the
-- visible text or the title of an album the photo is in changes (a trigger
-- cannot see the albums), with the expression below. The "simple"
-- configuration stems nothing and drops no words, because one library mixes
-- languages and a person searches in their own words.
UPDATE "photo" SET "search_document" =
    setweight(to_tsvector('simple', "place_name"), 'A') ||
    setweight(to_tsvector('simple', COALESCE((
        SELECT string_agg("photo_album"."album_title", ' ')
        FROM "photo_album_item" JOIN "photo_album" ON "photo_album"."id" = "photo_album_item"."photo_album_id"
        WHERE "photo_album_item"."photo_id" = "photo"."id"), '')), 'A') ||
    setweight(to_tsvector('simple', "photo_description" || ' ' || "description_visible_text"), 'B') ||
    setweight(to_tsvector('simple', "camera_make" || ' ' || "camera_model" || ' ' || "original_filename"), 'C');
CREATE INDEX "photo_search_document" ON "photo" USING gin ("search_document");

-- The meaning of a description, for finding a photo by what is in it when the
-- words differ. Like every other vector table: a real[] beside the name of
-- the model that wrote it, with the pgvector index built by the server at
-- start when the extension is there. "user_id" is the scope a search narrows
-- by.
CREATE TABLE "photo_vector" (
    "photo_id"   character varying(32)    NOT NULL REFERENCES "photo" ("id") ON DELETE CASCADE,
    "user_id"    character varying(32)    NOT NULL,
    "model"      character varying(200)   NOT NULL,
    "vector"     real[]                   NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("photo_id", "model")
);
ALTER TABLE "photo_vector" ALTER COLUMN "vector" SET STORAGE EXTERNAL;
CREATE INDEX "photo_vector_scope" ON "photo_vector" ("user_id", "model");
