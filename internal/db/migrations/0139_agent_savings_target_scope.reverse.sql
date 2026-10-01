-- A target that chose finance accounts keeps the assets those accounts
-- value today as assets of its own, so it still measures what it did, less
-- whatever an account comes to hold later. A net_worth target has no
-- equivalent in the older schema and is deleted.
INSERT INTO "agent_savings_target_asset" ("savings_target_id", "asset_id")
SELECT "chosen"."savings_target_id", "asset"."id"
FROM "agent_savings_target_finance_account" AS "chosen"
JOIN "agent_asset" AS "asset" ON "asset"."finance_account_id" = "chosen"."finance_account_id"
ON CONFLICT DO NOTHING;
DROP TABLE IF EXISTS "agent_savings_target_finance_account";
DELETE FROM "agent_savings_target" WHERE "target_measure" = 'net_worth';
ALTER TABLE "agent_savings_target" DROP CONSTRAINT IF EXISTS "agent_savings_target_target_measure";
ALTER TABLE "agent_savings_target" ADD CONSTRAINT "agent_savings_target_target_measure_check"
    CHECK ("target_measure" IN ('cash_flow', 'asset_value'));
