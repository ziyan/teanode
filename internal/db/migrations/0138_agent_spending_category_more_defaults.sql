-- Five more default spending categories (education, children, business
-- services, taxes, loans) for the agents that already have their defaults,
-- which new agents get from the start. A name the person already has, in
-- any case, is left alone. Each row carries a fixed created_at so the
-- reverse removes these and nothing the person made.
INSERT INTO "agent_spending_category" ("id", "agent_id", "spending_category_name", "is_income", "is_hidden", "created_at", "modified_at")
SELECT substr(md5("agents"."agent_id" || '/' || "names"."spending_category_name"), 1, 26), "agents"."agent_id",
       "names"."spending_category_name", false, false, '2026-09-30 00:00:00+00', now()
FROM (SELECT DISTINCT "agent_id" FROM "agent_spending_category") AS "agents"
CROSS JOIN (VALUES ('education'), ('children'), ('business services'), ('taxes'), ('loans')) AS "names" ("spending_category_name")
WHERE NOT EXISTS (
    SELECT 1 FROM "agent_spending_category" AS "existing"
    WHERE "existing"."agent_id" = "agents"."agent_id"
      AND lower("existing"."spending_category_name") = "names"."spending_category_name"
);
