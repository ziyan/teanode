-- A conversation names itself as it goes: a title and a line of summary
-- the model rewrites as the subject moves, unless the person named it
-- themselves, in which case the name is theirs and only the summary moves.
ALTER TABLE "agent_conversation" ADD COLUMN "summary" text NOT NULL DEFAULT '';
ALTER TABLE "agent_conversation" ADD COLUMN "titled_by" character varying(16) NOT NULL DEFAULT '';
