-- A card's credit limit, where the provider says what it is (Plaid's
-- balances.limit). Credit usage reads it, and where it is empty works the
-- limit out from what is owed plus the credit still available.
ALTER TABLE "agent_finance_account" ADD COLUMN "credit_limit_amount" numeric(19,4);
