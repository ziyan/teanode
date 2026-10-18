-- A person can take a photo's location away. is_location_removed says they
-- did: reading the file again (a Live Photo's video arriving, a retry) must not
-- put the camera's coordinates and place name back.
ALTER TABLE "photo" ADD COLUMN "is_location_removed" boolean NOT NULL DEFAULT false;
