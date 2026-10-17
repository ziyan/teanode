-- Albums: named sets of a person's photos.
--
-- An album holds references, never copies: a photo can be in several albums,
-- and deleting an album leaves every photo in the library. Taking a photo out
-- of the library (deleting it forever) takes it out of its albums with it,
-- but moving it to the trash does not: it stays in its albums, hidden, and
-- comes back with them when it is restored.
--
-- "cover_photo_id" is the photo the person chose to stand for the album. It is
-- null until they choose one, and the album shows its newest photo then, so
-- a photo that goes away only sends the album back to that.
--
-- "added_by_user_id" is who put the photo in. For now that is always the
-- album's owner; it is kept from the start so that an album people contribute
-- to later can tell whose photo is whose without a second pass over the rows.
CREATE TABLE "photo_album" (
    "id" varchar(32) PRIMARY KEY,
    "user_id" varchar(32) NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
    "album_title" varchar(255) NOT NULL,
    "cover_photo_id" varchar(32) REFERENCES "photo"("id") ON DELETE SET NULL,
    "created_at" timestamptz NOT NULL,
    "updated_at" timestamptz NOT NULL
);
-- The Albums page lists the most recently changed first.
CREATE INDEX "photo_album_owner" ON "photo_album" ("user_id", "updated_at" DESC);

CREATE TABLE "photo_album_item" (
    "photo_album_id" varchar(32) NOT NULL REFERENCES "photo_album"("id") ON DELETE CASCADE,
    "photo_id" varchar(32) NOT NULL REFERENCES "photo"("id") ON DELETE CASCADE,
    "added_by_user_id" varchar(32) NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
    "added_at" timestamptz NOT NULL,
    PRIMARY KEY ("photo_album_id", "photo_id")
);
-- Which albums a photo is in, and which photos are in no album.
CREATE INDEX "photo_album_item_photo" ON "photo_album_item" ("photo_id");
