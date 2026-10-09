-- A match was taken off the receipt by hand: the receipt matcher no longer
-- matches it on its own to a charge a later sync brings. Without it, a
-- provider that does not link a pending charge to the posted one it became
-- would bring the posted one as a new charge, and the same match back.
ALTER TABLE "agent_finance_receipt" ADD COLUMN IF NOT EXISTS "is_left_to_person" boolean NOT NULL DEFAULT false;
