-- Catching up is called bootstrapping: what a first ingest needs, and
-- what the person asks for by that name.
ALTER TABLE "agent" RENAME COLUMN "dream_catch_up" TO "dream_bootstrap";
