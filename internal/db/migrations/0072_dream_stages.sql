-- What the two halves of a night did.
--
-- Consolidating what arrived is only part of what sleep is for. The
-- quiet half reweights links by what was used together and lets the weak
-- fall out of reach; the other half walks the graph and asks whether two
-- pages that are connected but not adjacent have a real relation. Then
-- rehearsal: the questions tomorrow is likely to bring, asked of memory
-- alone, so a gap is found at three in the morning rather than
-- mid-conversation.
--
-- Counted separately because they answer different questions. "Links
-- reweighted" says the arithmetic ran; "links found" says the graph
-- gained something nobody typed; "questions it could not answer" is the
-- only number here a person might act on.
ALTER TABLE "agent_dream" ADD COLUMN "strengthened" integer NOT NULL DEFAULT 0;
ALTER TABLE "agent_dream" ADD COLUMN "associated"   integer NOT NULL DEFAULT 0;
ALTER TABLE "agent_dream" ADD COLUMN "rehearsed"    integer NOT NULL DEFAULT 0;
ALTER TABLE "agent_dream" ADD COLUMN "gaps"         integer NOT NULL DEFAULT 0;
