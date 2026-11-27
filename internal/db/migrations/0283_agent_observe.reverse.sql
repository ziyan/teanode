-- Only the row the forward migration inserted. An operator may have given
-- agent:observe to a role of their own since; a blanket delete on the
-- permission key would take that away too, and a downgrade is not a decision
-- about who may read other people's conversations.
DELETE FROM "role_permission"
WHERE "permission_key" = 'agent:observe'
  AND "role_id" IN (SELECT "id" FROM "role" WHERE lower("name") = 'administrator');

-- The rows of reads do not fit the narrower column, and are dropped with it.
DELETE FROM "audit_event" WHERE length("action") > 8;
ALTER TABLE "audit_event" ALTER COLUMN "action" TYPE character varying(8);

DROP INDEX IF EXISTS "agent_message_failed_note";
