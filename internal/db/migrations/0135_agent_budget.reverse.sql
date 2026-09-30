DROP INDEX IF EXISTS "agent_alert_candidate_budget";
ALTER TABLE "agent_alert_candidate" DROP COLUMN IF EXISTS "budget_key";
DROP TABLE IF EXISTS "agent_savings_target_asset";
DROP TABLE IF EXISTS "agent_savings_target";
DROP TABLE IF EXISTS "agent_budget";
DROP TABLE IF EXISTS "agent_spending_rule";
DROP INDEX IF EXISTS "agent_finance_transaction_uncategorized";
ALTER TABLE "agent_finance_transaction"
    DROP CONSTRAINT IF EXISTS "agent_finance_transaction_categorized_by",
    DROP COLUMN IF EXISTS "is_transfer_set_by_person",
    DROP COLUMN IF EXISTS "is_transfer",
    DROP COLUMN IF EXISTS "categorization_confidence",
    DROP COLUMN IF EXISTS "categorized_by",
    DROP COLUMN IF EXISTS "spending_category_id";
DROP TABLE IF EXISTS "agent_spending_category";
