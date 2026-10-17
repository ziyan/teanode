-- A person's picture. The bytes are a file in storage under
-- avatar-user-<user id>-<unix nano>, so a replacement is a new file with a new
-- name and the old one can be deleted without a viewer's cached copy being
-- overwritten. avatar_updated_at is what the picture's address and ETag carry,
-- so a browser fetches the new one the moment it changes. Both NULL means the
-- person has no picture and is shown as an initial on a color of their own.
ALTER TABLE "user"
    ADD COLUMN "avatar_file_id"    character varying(64),
    ADD COLUMN "avatar_updated_at" timestamp with time zone;
