ALTER TABLE "agent_asset_valuation"
    DROP COLUMN IF EXISTS "cost_basis",
    DROP COLUMN IF EXISTS "unit_price",
    DROP COLUMN IF EXISTS "held_quantity";
-- Holdings cannot be told apart from their account's asset without the
-- security, and would break the one asset per finance account again.
DELETE FROM "agent_asset" WHERE "finance_security_id" IS NOT NULL;
DROP INDEX IF EXISTS "agent_asset_finance_account_security";
ALTER TABLE "agent_asset"
    DROP COLUMN IF EXISTS "finance_security_id",
    ADD CONSTRAINT "agent_asset_finance_account_id_key" UNIQUE ("finance_account_id");
DROP TABLE IF EXISTS "agent_finance_trade";
DROP TABLE IF EXISTS "agent_finance_security";
