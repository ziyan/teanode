-- Back to other as an ordinary spending category. What this loses: the
-- category stays, as an ordinary one under whatever name it has, with its
-- transactions, rules and budgets, including one 0147 made where the
-- agent had none; and the transactions 0147 moved there from the person's
-- choice of no spending category stay there, since they cannot be told
-- from ones the person filed under other themselves.
DROP INDEX IF EXISTS "agent_spending_category_other";
ALTER TABLE "agent_spending_category"
    DROP CONSTRAINT IF EXISTS "agent_spending_category_other_not_income",
    DROP CONSTRAINT IF EXISTS "agent_spending_category_other_top_level",
    DROP CONSTRAINT IF EXISTS "agent_spending_category_other_not_transfer",
    DROP COLUMN IF EXISTS "is_other";
