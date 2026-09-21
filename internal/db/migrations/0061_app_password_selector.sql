-- Which app password a sign-in is offering.
--
-- The username is the mailbox's address, so a mailbox with twenty devices has
-- twenty passwords behind one name, and a sign-in had to try them all: twenty
-- hashes, at a sixth of a second each, for one wrong guess. The rate limiter
-- counted that as one attempt, so a mailbox with devices on it was twenty
-- times as expensive to guess at as an empty one.
--
-- The password now carries a tag naming its own row, so a sign-in is one
-- lookup and one hash. The tag is not a secret: it says which password is
-- being offered, not what it is.
--
-- Empty for a password made before this existed. Those still work, by the old
-- route, and the old route is only taken when a mailbox still has one.
ALTER TABLE "mailbox_app_password" ADD COLUMN "selector" character varying(16) NOT NULL DEFAULT '';

CREATE UNIQUE INDEX "mailbox_app_password_selector" ON "mailbox_app_password" ("mailbox_id", "selector")
    WHERE "selector" <> '';
