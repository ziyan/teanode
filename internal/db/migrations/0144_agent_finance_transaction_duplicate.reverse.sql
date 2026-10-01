-- Every mirrored copy counts again, and the person's "count this one" is
-- forgotten. Dropping the columns drops their checks and the index.
ALTER TABLE "agent_finance_transaction"
    DROP COLUMN IF EXISTS "duplicate_of_transaction_id",
    DROP COLUMN IF EXISTS "duplicate_decided_by";
