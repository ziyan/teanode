-- An idea started in a conversation that was deleted stayed started,
-- pointing at nothing, and was never offered again. Deleting a
-- conversation now puts such an idea back on offer; this does the same for
-- the ones deleted before. An idea done or dismissed keeps what became of
-- it and loses only the link.
UPDATE "agent_idea" SET
    "idea_status" = 'open', "started_conversation_id" = '', "started_at" = NULL, "closed_at" = NULL, "modified_at" = now()
WHERE "idea_status" = 'started' AND "started_conversation_id" <> ''
  AND NOT EXISTS (SELECT 1 FROM "agent_conversation" WHERE "agent_conversation"."id" = "agent_idea"."started_conversation_id");
UPDATE "agent_idea" SET "started_conversation_id" = '', "modified_at" = now()
WHERE "started_conversation_id" <> ''
  AND NOT EXISTS (SELECT 1 FROM "agent_conversation" WHERE "agent_conversation"."id" = "agent_idea"."started_conversation_id");
