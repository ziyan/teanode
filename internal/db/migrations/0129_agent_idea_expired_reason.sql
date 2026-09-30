-- Why an expired idea is not on offer: past_date, missing_tool or
-- already_used, and empty for an idea in any other status.
ALTER TABLE "agent_idea" ADD COLUMN "expired_reason" character varying(20) NOT NULL DEFAULT '';
-- A catalog idea the person opened again after it expired because they
-- already do what it offers; the check that found that no longer expires it.
ALTER TABLE "agent_idea" ADD COLUMN "is_restored_by_person" boolean NOT NULL DEFAULT false;

-- A personal idea expires only when its date passes, so the ones already
-- expired can say so. An expired catalog idea is given its reason the next
-- time the catalog is read, which works out why it is not offered now.
UPDATE "agent_idea" SET "expired_reason" = 'past_date'
WHERE "idea_status" = 'expired' AND "idea_kind" = 'personal';
