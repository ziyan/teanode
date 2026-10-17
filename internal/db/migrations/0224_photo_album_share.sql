-- An album can be shared with a person or with a circle, and a circle can
-- have albums of its own.
--
-- photo_album.user_id becomes nullable and photo_album.circle_id is added
-- beside it: exactly one of the two is set. An album with a circle_id belongs
-- to the circle, is added to by every member, and goes when the circle goes.
-- The photos in it stay the photos of whoever added them (photo_album_item
-- already says who), so the album holds references as every album does.
ALTER TABLE "photo_album" ALTER COLUMN "user_id" DROP NOT NULL;
ALTER TABLE "photo_album" ADD COLUMN "circle_id" varchar(32) REFERENCES "circle" ("id") ON DELETE CASCADE;
ALTER TABLE "photo_album" ADD CONSTRAINT "photo_album_one_owner" CHECK (num_nonnulls("user_id", "circle_id") = 1);
CREATE INDEX "photo_album_circle" ON "photo_album" ("circle_id", "updated_at" DESC) WHERE "circle_id" IS NOT NULL;

-- One row gives one person, or every member of one circle, a level of access
-- to one album. share_level is view (see and save its photos) or contribute
-- (also add photos of their own and take out the ones they added).
-- is_location_included says whether the owner lets them see where the photos
-- were taken, and receive the original files; without it they get the large
-- preview, which carries no place.
--
-- Sharing with a person needs the owner and that person to be in a circle
-- together, and sharing with a circle needs the owner to be a member of it.
-- That is checked when the share is made. What keeps it true afterwards is not
-- this table: a circle's share reaches only members whose circle the album's
-- owner is still in, by the join on circle_member, and the code that takes a
-- person out of a circle deletes the direct shares between people who no
-- longer share one, and the photos they put in albums they can no longer
-- reach. A circle's own albums are never shared further: every member has
-- them already.
CREATE TABLE "photo_album_share" (
    "id"                   varchar(32) NOT NULL PRIMARY KEY,
    "photo_album_id"       varchar(32) NOT NULL REFERENCES "photo_album" ("id") ON DELETE CASCADE,
    "grantee_user_id"      varchar(32) REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_circle_id"    varchar(32) REFERENCES "circle" ("id") ON DELETE CASCADE,
    "share_level"          varchar(32) NOT NULL
        CHECK ("share_level" IN ('view', 'contribute')),
    "is_location_included" boolean NOT NULL DEFAULT false,
    "created_by_user_id"   varchar(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "created_at"           timestamptz NOT NULL,
    CHECK (num_nonnulls("grantee_user_id", "grantee_circle_id") = 1),
    UNIQUE ("photo_album_id", "grantee_user_id"),
    UNIQUE ("photo_album_id", "grantee_circle_id")
);

-- "Which albums are shared with me" starts from the grantee.
CREATE INDEX "photo_album_share_grantee_user" ON "photo_album_share" ("grantee_user_id") WHERE "grantee_user_id" IS NOT NULL;
CREATE INDEX "photo_album_share_grantee_circle" ON "photo_album_share" ("grantee_circle_id") WHERE "grantee_circle_id" IS NOT NULL;
