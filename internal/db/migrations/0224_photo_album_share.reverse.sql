-- Going back is refused while a circle has an album: it has no account to
-- belong to, and deleting a family's album is a decision for a person, not for
-- a rollback. Its photos are in the libraries of the people who added them, so
-- nothing but the album is lost: delete the circle albums, and then roll back.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM "photo_album" WHERE "user_id" IS NULL) THEN
        RAISE EXCEPTION 'a circle has a photo album of its own; delete it before going back (its photos stay in the libraries of the people who added them)';
    END IF;
END
$$;

DROP TABLE "photo_album_share";
DROP INDEX "photo_album_circle";
ALTER TABLE "photo_album" DROP CONSTRAINT "photo_album_one_owner";
ALTER TABLE "photo_album" DROP COLUMN "circle_id";
ALTER TABLE "photo_album" ALTER COLUMN "user_id" SET NOT NULL;
