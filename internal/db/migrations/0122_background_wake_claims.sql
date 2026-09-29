-- Two instances could each wake a conversation for the same finished
-- background work: the one that ran it, and another's sweep a minute
-- later while the first turn was still going. The one that sets
-- wake_claimed_at first wakes it; a claim older than half an hour belongs
-- to a server that went down mid-wake and may be taken again.
ALTER TABLE "agent_background_work" ADD COLUMN "wake_claimed_at" timestamp with time zone;

-- How many turns ended background commands and finished background work
-- have woken in a conversation since the person last wrote in it. Kept
-- here rather than in one instance's memory, so that the bound on a chain
-- of woken turns holds whichever instance wakes the next one.
ALTER TABLE "agent_conversation" ADD COLUMN "background_wake_count" integer NOT NULL DEFAULT 0;
