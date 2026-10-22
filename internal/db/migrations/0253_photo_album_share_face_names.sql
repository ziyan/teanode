-- A share says whether the people it reaches see the names the owner gave the
-- faces in the album's photos, as is_location_included says whether they see
-- where the photos were taken. The names are the owner's own address book
-- entries, so without this a reader gets the boxes round the faces and no
-- name, contact or group. Existing shares keep what they gave: no names.
ALTER TABLE "photo_album_share" ADD COLUMN "is_face_names_included" boolean NOT NULL DEFAULT false;
