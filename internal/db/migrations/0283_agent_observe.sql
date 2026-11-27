-- The Administrator role gains agent:observe: every person's agent
-- conversations and runs, read-only, each read audited. Content, so the
-- Operator role does not get it. Once, here, so an operator who later takes
-- it away is not overruled at every start.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'agent:observe' FROM "role" WHERE lower("name") = 'administrator'
ON CONFLICT DO NOTHING;

-- An audit row for a read is named agent.conversation.observe, which the
-- eight characters of create, update and delete cannot hold.
ALTER TABLE "audit_event" ALTER COLUMN "action" TYPE character varying(32);

-- The Oversight list of failed turns reads the notes that say a turn failed,
-- newest first, without walking every message there is.
CREATE INDEX IF NOT EXISTS "agent_message_failed_note" ON "agent_message" ("created_at" DESC)
WHERE "role" = 'note' AND "name" = 'failed';
