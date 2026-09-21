-- What a source's last pass read of the checkouts under it, and what it
-- left alone.
--
-- A files source pointed at a folder of checkouts used to index every
-- one of them, including the ones the person had only cloned, and the
-- agent spent its nights filing facts about somebody else's source onto
-- the person's own pages. A checkout with none of the person's commits
-- in it is now kept to its profile: what it is, where it lives, and what
-- git says about it, with its files left unread.
--
-- Those two numbers are the difference, and they are on the row because
-- reading less than a person asked for is not something to do quietly.
-- The source's page and its command line row say how many checkouts were
-- kept to their profile and how many files that was, and the source's
-- own specification.readEveryCheckout turns it off.
ALTER TABLE "agent_source" ADD COLUMN "checkouts_kept_to_profile" integer NOT NULL DEFAULT 0;
ALTER TABLE "agent_source" ADD COLUMN "files_kept_to_profile" integer NOT NULL DEFAULT 0;
