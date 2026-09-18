-- What an edge means, and when anything was last true.
--
-- An edge used to be a bare word: "works_on". A word is enough to walk
-- the graph with and not enough to read: the model is shown
-- "people/alice-chen works_on projects/portal" and has to guess what that
-- was ever supposed to say. So an edge carries a note -- the particular
-- thing this link is about, in the person's own terms -- and the relation
-- itself is rendered as a phrase rather than a token.
ALTER TABLE "agent_edge" ADD COLUMN "note" text NOT NULL DEFAULT '';

-- When the link was last true, so a join that has gone stale sinks rather
-- than being forgotten. Somebody who left a project two years ago did
-- work on it; the fact is still true and no longer worth putting in front
-- of the model first.
ALTER TABLE "agent_edge" ADD COLUMN "happened_at" timestamp with time zone;
ALTER TABLE "agent_edge" ADD COLUMN "used_at" timestamp with time zone;

CREATE INDEX "agent_edge_from" ON "agent_edge" ("from_id", "relation");
