DELETE FROM "agent_spending_category"
WHERE "created_at" = '2026-09-30 00:00:00+00'
  AND "spending_category_name" IN ('education', 'children', 'business services', 'taxes', 'loans');
