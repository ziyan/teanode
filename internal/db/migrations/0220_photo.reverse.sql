DELETE FROM "role_permission" WHERE "permission_key" = 'photos:use';
ALTER TABLE "sync_credential" DROP COLUMN "has_photos_access";
DROP TABLE "photo_preview";
DROP TABLE "photo_upload_part";
DROP TABLE "photo_upload";
DROP TABLE "photo_file";
DROP TABLE "photo_blob";
DROP TABLE "photo";
