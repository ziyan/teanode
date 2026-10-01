-- A savings target can be measured by net worth (net_worth), and an
-- asset_value target can choose whole finance accounts as well as single
-- assets. A chosen finance account counts every asset it values, read when
-- the progress is read, so a holding bought after the target started counts
-- without the target being edited.
ALTER TABLE "agent_savings_target" DROP CONSTRAINT IF EXISTS "agent_savings_target_target_measure_check";
ALTER TABLE "agent_savings_target" ADD CONSTRAINT "agent_savings_target_target_measure"
    CHECK ("target_measure" IN ('cash_flow', 'asset_value', 'net_worth'));

-- The finance accounts an asset_value savings target measures. Gone with
-- the finance account, as its assets stop counting once it is deleted.
CREATE TABLE "agent_savings_target_finance_account" (
    "savings_target_id"  character varying(32) NOT NULL REFERENCES "agent_savings_target" ("id") ON DELETE CASCADE,
    "finance_account_id" character varying(32) NOT NULL REFERENCES "agent_finance_account" ("id") ON DELETE CASCADE,
    PRIMARY KEY ("savings_target_id", "finance_account_id")
);
