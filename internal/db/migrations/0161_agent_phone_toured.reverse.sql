-- A phone not yet shown around is offered the tour again after a later
-- upgrade, which says nothing it would not have said the first time.
ALTER TABLE "agent_phone" DROP COLUMN "toured_at";
