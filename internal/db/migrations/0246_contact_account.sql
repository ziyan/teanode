-- Which account on this server a contact is: the owner's own note that their
-- contact "Sam" is the person with the account sam, who shares a circle with
-- them. It is chosen by the owner and never made silently. NULL for a contact
-- that is not an account here.
--
-- Only the owner sees it, and only while they share a circle with that
-- account: it is cleared in the transaction in which the two stop sharing any
-- circle, as a share between them is, and checked when it is read. The linked
-- person is never told and sees nothing of it. Address books are never shared,
-- so the owner is the one who owns the contact's address book.
--
-- Whether the account is the owner's own, or one they share no circle with,
-- is refused where the link is written (the owner is reached through
-- addressbook.user_id, so neither can be a CHECK here). The account going away
-- only clears the link; it does not delete the contact.
ALTER TABLE "contact" ADD COLUMN "account_user_id" character varying(32) NULL
    REFERENCES "user" ("id") ON DELETE SET NULL;

-- One contact per account in each address book, so "photos of Sam" is never
-- ambiguous. Two books of the same person may each hold one.
CREATE UNIQUE INDEX "contact_addressbook_account_user" ON "contact" ("addressbook_id", "account_user_id")
    WHERE "account_user_id" IS NOT NULL;
-- Ending links when two people stop sharing a circle looks them up from the
-- account's side; the unique index above leads with the address book.
CREATE INDEX "contact_account_user" ON "contact" ("account_user_id") WHERE "account_user_id" IS NOT NULL;
