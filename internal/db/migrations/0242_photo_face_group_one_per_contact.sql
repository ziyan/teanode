-- A contact names one person. Before this a contact could name several of the
-- person's face groups, and the person had to merge them by hand. Naming now
-- merges them; this does the same once for the groups already named with one
-- contact: the oldest group of each contact stays, takes the faces of the
-- others, keeps its own cover, and is hidden only when all of them were. The
-- vector of the group that stays is made again from its faces, as a merge does.

-- The groups that stay and take faces from others: the oldest group of a
-- contact that names more than one. Found before anything moves, because the
-- steps below must touch these groups and no others. Dropped at the end.
CREATE TEMPORARY TABLE "photo_face_group_folded" AS
SELECT "kept"."id" FROM "photo_face_group" "kept"
WHERE "kept"."contact_id" IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM "photo_face_group" "earlier"
      WHERE "earlier"."user_id" = "kept"."user_id" AND "earlier"."addressbook_id" = "kept"."addressbook_id" AND "earlier"."contact_id" = "kept"."contact_id"
        AND ("earlier"."created_at", "earlier"."id") < ("kept"."created_at", "kept"."id"))
  AND EXISTS (
      SELECT 1 FROM "photo_face_group" "later"
      WHERE "later"."user_id" = "kept"."user_id" AND "later"."addressbook_id" = "kept"."addressbook_id" AND "later"."contact_id" = "kept"."contact_id"
        AND "later"."id" <> "kept"."id");

-- A group that stays is shown when any of the groups put into it was.
UPDATE "photo_face_group" "kept" SET "is_hidden" = false, "updated_at" = now()
WHERE "kept"."is_hidden" AND "kept"."id" IN (SELECT "id" FROM "photo_face_group_folded")
  AND EXISTS (
      SELECT 1 FROM "photo_face_group" "later"
      WHERE "later"."user_id" = "kept"."user_id" AND "later"."addressbook_id" = "kept"."addressbook_id" AND "later"."contact_id" = "kept"."contact_id"
        AND "later"."id" <> "kept"."id" AND NOT "later"."is_hidden");

UPDATE "photo_face" SET "photo_face_group_id" = (
    SELECT "kept"."id" FROM "photo_face_group" "kept"
    WHERE "kept"."user_id" = "gone"."user_id" AND "kept"."addressbook_id" = "gone"."addressbook_id" AND "kept"."contact_id" = "gone"."contact_id"
    ORDER BY "kept"."created_at", "kept"."id" LIMIT 1)
FROM "photo_face_group" "gone"
WHERE "photo_face"."photo_face_group_id" = "gone"."id" AND "gone"."contact_id" IS NOT NULL
  AND EXISTS (
      SELECT 1 FROM "photo_face_group" "earlier"
      WHERE "earlier"."user_id" = "gone"."user_id" AND "earlier"."addressbook_id" = "gone"."addressbook_id" AND "earlier"."contact_id" = "gone"."contact_id"
        AND ("earlier"."created_at", "earlier"."id") < ("gone"."created_at", "gone"."id"));

-- The groups that stay and took faces, whose vectors and counts are made again.
UPDATE "photo_face_group" SET
    "group_vector_sum" = COALESCE((
        SELECT array_agg("sums"."total" ORDER BY "sums"."position") FROM (
            SELECT "numbers"."position", sum("numbers"."number") AS "total"
            FROM "photo_face", unnest("photo_face"."face_vector") WITH ORDINALITY AS "numbers"("number", "position")
            WHERE "photo_face"."photo_face_group_id" = "photo_face_group"."id"
            GROUP BY "numbers"."position"
        ) "sums"), "photo_face_group"."group_vector_sum"),
    "face_group_vector_count" = (SELECT count(*) FROM "photo_face" WHERE "photo_face"."photo_face_group_id" = "photo_face_group"."id"),
    "updated_at" = now()
WHERE "id" IN (SELECT "id" FROM "photo_face_group_folded");

-- A photo does not hold one person twice, and grouping never puts two faces of
-- one photo in a group; two groups of one person can each have a different face
-- of the same photo. In every group that stays, of the faces of a photo the one
-- kept is the one the person confirmed, else the one most like the group's vector (cosine
-- similarity with the sum of all its faces). The others are in no group and not
-- confirmed, as a face too small to group is, and the group's vector, count
-- and cover are made again from the faces that stay.
WITH "ranked" AS (
    SELECT "photo_face"."id", "photo_face"."photo_face_group_id" AS "group_id",
           row_number() OVER (
               PARTITION BY "photo_face"."photo_id", "photo_face"."photo_face_group_id"
               ORDER BY "photo_face"."is_group_confirmed" DESC, "similarity"."value" DESC, "photo_face"."id") AS "place"
    FROM "photo_face"
    JOIN "photo_face_group" ON "photo_face_group"."id" = "photo_face"."photo_face_group_id"
    CROSS JOIN LATERAL (
        SELECT COALESCE(sum("pair"."face_number"::float8 * "pair"."group_number"::float8)
                   / NULLIF(sqrt(sum("pair"."face_number"::float8 ^ 2)) * sqrt(sum("pair"."group_number"::float8 ^ 2)), 0), 0) AS "value"
        FROM unnest("photo_face"."face_vector", "photo_face_group"."group_vector_sum") AS "pair"("face_number", "group_number")
    ) "similarity"
    WHERE "photo_face"."photo_face_group_id" IN (SELECT "id" FROM "photo_face_group_folded")
), "leaving" AS (
    SELECT "id", "group_id" FROM "ranked" WHERE "place" > 1
), "removed" AS (
    UPDATE "photo_face" SET "photo_face_group_id" = NULL, "is_group_confirmed" = false
    FROM "leaving" WHERE "photo_face"."id" = "leaving"."id"
    RETURNING "photo_face"."id"
)
UPDATE "photo_face_group" SET
    "group_vector_sum" = COALESCE((
        SELECT array_agg("sums"."total" ORDER BY "sums"."position") FROM (
            SELECT "numbers"."position", sum("numbers"."number") AS "total"
            FROM "photo_face", unnest("photo_face"."face_vector") WITH ORDINALITY AS "numbers"("number", "position")
            WHERE "photo_face"."photo_face_group_id" = "photo_face_group"."id" AND "photo_face"."id" NOT IN (SELECT "id" FROM "leaving")
            GROUP BY "numbers"."position"
        ) "sums"), "photo_face_group"."group_vector_sum"),
    "face_group_vector_count" = (
        SELECT count(*) FROM "photo_face"
        WHERE "photo_face"."photo_face_group_id" = "photo_face_group"."id" AND "photo_face"."id" NOT IN (SELECT "id" FROM "leaving")),
    "cover_photo_face_id" = CASE WHEN "cover_photo_face_id" IN (SELECT "id" FROM "leaving") THEN NULL ELSE "cover_photo_face_id" END,
    "updated_at" = now()
WHERE "id" IN (SELECT "group_id" FROM "leaving");

DELETE FROM "photo_face_group" "gone"
WHERE "gone"."contact_id" IS NOT NULL
  AND EXISTS (
      SELECT 1 FROM "photo_face_group" "earlier"
      WHERE "earlier"."user_id" = "gone"."user_id" AND "earlier"."addressbook_id" = "gone"."addressbook_id" AND "earlier"."contact_id" = "gone"."contact_id"
        AND ("earlier"."created_at", "earlier"."id") < ("gone"."created_at", "gone"."id"));

DROP TABLE "photo_face_group_folded";
