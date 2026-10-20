-- Which contact a confirmed speaker name is: the owner's own note that the
-- voice they named "Sam" is their contact Sam, so the contact's card can list
-- the recordings that voice speaks in. It is chosen by the owner when they name
-- the speaker or afterwards, and is never made silently. NULL for a name that
-- is not tied to a contact.
--
-- Both columns are set or both are NULL, as a contact is named by its address
-- book and its own ID together. The contact being deleted, or its address book,
-- only unties the name; the name and everything it labels stay.
--
-- That the address book belongs to the account that owns the name is checked
-- where the tie is written (the owner is reached through addressbook.user_id,
-- so it cannot be a CHECK here).
ALTER TABLE "recording_speaker_identity" ADD COLUMN "contact_addressbook_id" character varying(32) NULL;
ALTER TABLE "recording_speaker_identity" ADD COLUMN "contact_id" character varying(255) NULL;
ALTER TABLE "recording_speaker_identity" ADD CONSTRAINT "recording_speaker_identity_contact_both_or_neither"
    CHECK (("contact_addressbook_id" IS NULL) = ("contact_id" IS NULL));
ALTER TABLE "recording_speaker_identity" ADD CONSTRAINT "recording_speaker_identity_contact_fkey"
    FOREIGN KEY ("contact_addressbook_id", "contact_id") REFERENCES "contact" ("addressbook_id", "id") ON DELETE SET NULL;

-- Listing the recordings a contact speaks in, and clearing the tie when a
-- contact is deleted, both look the names up from the contact's side.
CREATE INDEX "recording_speaker_identity_contact" ON "recording_speaker_identity" ("contact_addressbook_id", "contact_id")
    WHERE "contact_id" IS NOT NULL;
