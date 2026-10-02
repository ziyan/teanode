-- Spending rules that match exactly what an earlier rule matches: the same
-- words, whatever their case, the same finance account and the same amount
-- limits. Saving a rule from a transaction added a copy each time it was
-- saved. Rules are tried in order and the first that matches wins, so the
-- later copies never applied; deleting them changes no transaction's
-- category. The earliest in order is kept.
DELETE FROM "agent_spending_rule" AS "later"
USING "agent_spending_rule" AS "earlier"
WHERE "later"."agent_id" = "earlier"."agent_id"
    AND lower("later"."match_text") = lower("earlier"."match_text")
    AND "later"."finance_account_id" IS NOT DISTINCT FROM "earlier"."finance_account_id"
    AND "later"."minimum_amount" IS NOT DISTINCT FROM "earlier"."minimum_amount"
    AND "later"."maximum_amount" IS NOT DISTINCT FROM "earlier"."maximum_amount"
    AND ("earlier"."rule_priority", "earlier"."created_at", "earlier"."id")
        < ("later"."rule_priority", "later"."created_at", "later"."id");
