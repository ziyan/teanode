-- A coding session starting in a directory asks for the last session that
-- was held there. The chat units of coding tools name their working
-- directory in their metadata, and without an index on it the question
-- read every document the agent has, newest first, before it could say
-- there were none: 2.5 seconds on a million documents, at the start of
-- every session.
CREATE INDEX "agent_document_directory" ON "agent_document"
    ("agent_id", ("metadata"->>'directory'), "happened_at" DESC NULLS LAST)
    WHERE "kind" = 'chat' AND ("metadata"->>'directory') IS NOT NULL;
