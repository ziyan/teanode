-- A circle can have an address book of its own, kept by the people in it.
--
-- addressbook.user_id becomes nullable and addressbook.circle_id is added
-- beside it: exactly one of the two is set. A book with a circle_id belongs to
-- the circle, is read and edited by every member, and goes when the circle
-- goes. A circle has one at most. A person's own address book is still never
-- shared: nothing here reaches a book with a user_id.
--
-- Who may use a circle's book is decided where it is read, by the join on
-- circle_member, so a person who leaves the circle loses it in the same
-- transaction and no row has to be cleaned up.
ALTER TABLE "addressbook" ALTER COLUMN "user_id" DROP NOT NULL;
ALTER TABLE "addressbook" ADD COLUMN "circle_id" character varying(32) REFERENCES "circle" ("id") ON DELETE CASCADE;
ALTER TABLE "addressbook" ADD CONSTRAINT "addressbook_one_owner" CHECK (num_nonnulls("user_id", "circle_id") = 1);
CREATE UNIQUE INDEX "addressbook_one_per_circle" ON "addressbook" ("circle_id") WHERE "circle_id" IS NOT NULL;
