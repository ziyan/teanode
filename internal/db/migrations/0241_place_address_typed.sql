-- A person can keep an address exactly as they typed it, for a place the map
-- does not know. is_address_typed says the address_text of the place is that:
-- moving the place afterwards (to put its pin on the house) keeps it, and the
-- naming job never replaces it. Typing an empty address takes it away.
ALTER TABLE "place" ADD COLUMN "is_address_typed" boolean NOT NULL DEFAULT false;
