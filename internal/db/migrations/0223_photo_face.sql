-- Faces found in photos, and the people they are grouped into.
--
-- A face is found by a separate program that reads a photo's large preview
-- and gives each face a box and a vector of 128 numbers: two faces of the
-- same person have vectors pointing the same way. The box is kept as
-- fractions of the picture, so it means the same on the preview and on the
-- original.
--
-- A face group is the faces the server believes are one person. It is nobody
-- until the person names it, and naming it means choosing a contact from the
-- address book. A contact is only unique inside its address book (its key is
-- both columns), so the group keeps both, and the contact going away leaves
-- the group unnamed rather than deleting it.
--
-- "group_vector_sum" is the sum of the unit vectors of the faces in the group.
-- Its direction is the average direction of the group, which is all a
-- similarity needs, and a sum can take one more face without reading the
-- others. "face_group_vector_count" is how many vectors went into it.
--
-- "is_group_confirmed" says the person put the face in its group, or took it
-- out of another one, so it is never grouped again. A face that is too small
-- or too uncertain to group has no group and is not confirmed.
--
-- "faces_found_at" is when a photo's faces were found, null until then; the
-- photos that wait for their faces are the ready ones where it is null.
-- "is_photo_face_recognition_enabled" is the person's own switch. Switching it
-- off deletes every face and group they have.
ALTER TABLE "photo" ADD COLUMN "faces_found_at" timestamptz;

ALTER TABLE "user" ADD COLUMN "is_photo_face_recognition_enabled" boolean NOT NULL DEFAULT true;

CREATE TABLE "photo_face_group" (
    "id" varchar(32) PRIMARY KEY,
    "user_id" varchar(32) NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
    "addressbook_id" varchar(32),
    "contact_id" varchar(255),
    "cover_photo_face_id" varchar(32),
    "group_vector_sum" real[] NOT NULL,
    "face_group_vector_count" integer NOT NULL DEFAULT 0,
    "is_hidden" boolean NOT NULL DEFAULT false,
    "created_at" timestamptz NOT NULL,
    "updated_at" timestamptz NOT NULL,
    CHECK (("addressbook_id" IS NULL) = ("contact_id" IS NULL)),
    FOREIGN KEY ("addressbook_id", "contact_id") REFERENCES "contact"("addressbook_id", "id") ON DELETE SET NULL
);
ALTER TABLE "photo_face_group" ALTER COLUMN "group_vector_sum" SET STORAGE EXTERNAL;
CREATE INDEX "photo_face_group_owner" ON "photo_face_group" ("user_id");
-- Which groups a contact names, for the photos of a person.
CREATE INDEX "photo_face_group_contact" ON "photo_face_group" ("user_id", "contact_id") WHERE "contact_id" IS NOT NULL;

CREATE TABLE "photo_face" (
    "id" varchar(32) PRIMARY KEY,
    "photo_id" varchar(32) NOT NULL REFERENCES "photo"("id") ON DELETE CASCADE,
    "user_id" varchar(32) NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
    "box_left_fraction" real NOT NULL,
    "box_top_fraction" real NOT NULL,
    "box_width_fraction" real NOT NULL,
    "box_height_fraction" real NOT NULL,
    "detection_score" real NOT NULL,
    "face_vector" real[] NOT NULL,
    "photo_face_group_id" varchar(32) REFERENCES "photo_face_group"("id") ON DELETE SET NULL,
    "is_group_confirmed" boolean NOT NULL DEFAULT false,
    "created_at" timestamptz NOT NULL
);
ALTER TABLE "photo_face" ALTER COLUMN "face_vector" SET STORAGE EXTERNAL;
CREATE INDEX "photo_face_photo" ON "photo_face" ("photo_id");
CREATE INDEX "photo_face_by_group" ON "photo_face" ("photo_face_group_id") WHERE "photo_face_group_id" IS NOT NULL;
CREATE INDEX "photo_face_owner" ON "photo_face" ("user_id");

-- A group's cover is one of its faces; if the face goes the group is shown by
-- another.
ALTER TABLE "photo_face_group" ADD FOREIGN KEY ("cover_photo_face_id") REFERENCES "photo_face"("id") ON DELETE SET NULL;

-- The photos still waiting for their faces, for each person who has faces on.
CREATE INDEX "photo_faces_waiting" ON "photo" ("user_id", "created_at", "id")
    WHERE "photo_processing_state" = 'ready' AND "faces_found_at" IS NULL AND "trashed_at" IS NULL;
