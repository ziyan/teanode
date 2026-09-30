-- The five categories this added are taken away again. What was filed under
-- them is cleared first the way deleting a category from the dashboard
-- clears it, so each such transaction is uncategorized and can be
-- categorized again, rather than left saying who chose a category it no
-- longer has. Budgets and spending rules on these categories go with them.
UPDATE "agent_finance_transaction" SET "spending_category_id" = NULL, "categorized_by" = '',
    "categorization_confidence" = NULL, "categorize_attempted_at" = NULL, "modified_at" = now()
WHERE "spending_category_id" IN (
    SELECT "id" FROM "agent_spending_category"
    WHERE "created_at" = '2026-09-30 00:00:00+00'
      AND "spending_category_name" IN ('education', 'children', 'business services', 'taxes', 'loans'));
DELETE FROM "agent_spending_category"
WHERE "created_at" = '2026-09-30 00:00:00+00'
  AND "spending_category_name" IN ('education', 'children', 'business services', 'taxes', 'loans');
