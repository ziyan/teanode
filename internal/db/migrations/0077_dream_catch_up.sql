-- Whether the night runs again as soon as it can, until nothing waits.
--
-- A night reads a bounded number of documents and then rests for six
-- hours, which is right for a model that costs money and a backlog that
-- arrives a day at a time. A first ingest of years of chat is neither:
-- with a model of the person's own on their own machine, the reading
-- costs nothing but time, and six hours between two thousand documents
-- is months. Catch-up is the person saying so; the night switches it
-- off itself when the backlog is gone.
ALTER TABLE "agent" ADD COLUMN "dream_catch_up" boolean NOT NULL DEFAULT false;
