DROP INDEX "photo_faces_waiting";
ALTER TABLE "photo_face_group" DROP COLUMN "cover_photo_face_id";
DROP TABLE "photo_face";
DROP TABLE "photo_face_group";
ALTER TABLE "user" DROP COLUMN "is_photo_face_recognition_enabled";
ALTER TABLE "photo" DROP COLUMN "faces_found_at";
