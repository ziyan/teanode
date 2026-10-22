-- Going back is refused while a circle has an address book: it has no account
-- to belong to, and deleting a family's address book is a decision for a
-- person, not for a rollback. Copy the contacts out first (a phone's CardDAV
-- account holds a copy, and "teanode contact list" prints them), delete the
-- circle address books, and then roll back.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM "addressbook" WHERE "user_id" IS NULL) THEN
        RAISE EXCEPTION 'a circle has an address book of its own; copy its contacts out and delete it before going back';
    END IF;
END
$$;

DROP INDEX "addressbook_one_per_circle";
ALTER TABLE "addressbook" DROP CONSTRAINT "addressbook_one_owner";
ALTER TABLE "addressbook" DROP COLUMN "circle_id";
ALTER TABLE "addressbook" ALTER COLUMN "user_id" SET NOT NULL;
