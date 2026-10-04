-- Other becomes a built-in spending category, like transfer, and no
-- spending category stops being something the person chooses: a finance
-- transaction with none is one not decided yet, and the person's "fits
-- nothing" is the other category. Each agent has one: it cannot be
-- deleted, be income, sit under a parent or have children of its own. It
-- is spending like any other spending category.
ALTER TABLE "agent_spending_category"
    ADD COLUMN "is_other" boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT "agent_spending_category_other_not_income" CHECK (NOT ("is_other" AND "is_income")),
    ADD CONSTRAINT "agent_spending_category_other_top_level" CHECK (NOT ("is_other" AND "parent_spending_category_id" IS NOT NULL)),
    ADD CONSTRAINT "agent_spending_category_other_not_transfer" CHECK (NOT ("is_other" AND "is_transfer"));
CREATE UNIQUE INDEX "agent_spending_category_other" ON "agent_spending_category" ("agent_id") WHERE "is_other";

-- The default "other" becomes the built-in one, found by the name the
-- defaults store it under, exactly, so its transactions, rules and budget
-- stay where they are. One the person made income, put under a parent or
-- gave children of its own cannot be the built-in one and stays theirs; so
-- does one they renamed. Where this migration runs again after its
-- reverse, the one it made the first time (by its id) is the built-in one
-- again, ahead of a default "other".
UPDATE "agent_spending_category"
SET "is_other" = true
WHERE "id" IN (
    SELECT DISTINCT ON ("candidate"."agent_id") "candidate"."id"
    FROM "agent_spending_category" AS "candidate"
    WHERE ("candidate"."spending_category_name" = 'other' OR "candidate"."id" = substr(md5("candidate"."agent_id" || '/other'), 1, 26))
      AND NOT "candidate"."is_income" AND NOT "candidate"."is_transfer"
      AND "candidate"."parent_spending_category_id" IS NULL
      AND NOT EXISTS (
          SELECT 1 FROM "agent_spending_category" AS "child"
          WHERE "child"."parent_spending_category_id" = "candidate"."id"
      )
    ORDER BY "candidate"."agent_id", "candidate"."id" = substr(md5("candidate"."agent_id" || '/other'), 1, 26) DESC
);

-- One for every agent still without: named "other", or "anything else"
-- where the person has an "other" of their own (in any case), or that
-- with a few characters of its id after it where they have both. The id
-- is fixed by the agent's, unless a run before the reverse already made
-- one by it that could not be marked above.
WITH "missing" AS (
    SELECT "agent"."id" AS "agent_id",
           CASE WHEN EXISTS (
               SELECT 1 FROM "agent_spending_category" AS "earlier"
               WHERE "earlier"."id" = substr(md5("agent"."id" || '/other'), 1, 26)
           ) THEN substr(md5("agent"."id" || '/other/' || now()::text), 1, 26)
           ELSE substr(md5("agent"."id" || '/other'), 1, 26) END AS "id"
    FROM "agent"
    WHERE NOT EXISTS (
        SELECT 1 FROM "agent_spending_category" AS "built_in"
        WHERE "built_in"."agent_id" = "agent"."id" AND "built_in"."is_other"
    )
)
INSERT INTO "agent_spending_category" ("id", "agent_id", "spending_category_name", "is_income", "is_hidden", "is_transfer", "is_other", "created_at", "modified_at")
SELECT "missing"."id", "missing"."agent_id",
       CASE
           WHEN NOT EXISTS (
               SELECT 1 FROM "agent_spending_category" AS "existing"
               WHERE "existing"."agent_id" = "missing"."agent_id" AND lower("existing"."spending_category_name") = 'other'
           ) THEN 'other'
           WHEN NOT EXISTS (
               SELECT 1 FROM "agent_spending_category" AS "existing"
               WHERE "existing"."agent_id" = "missing"."agent_id" AND lower("existing"."spending_category_name") = 'anything else'
           ) THEN 'anything else'
           ELSE 'anything else ' || substr("missing"."id", 1, 6)
       END,
       false, false, false, true, now(), now()
FROM "missing";

-- The person's choice of no spending category was their "fits nothing",
-- and becomes their choice of the other category. What is not decided yet
-- (categorized_by empty) stays uncategorized, for the categorize model and
-- the person.
-- A transaction the person said is not a transfer, and that had no
-- category, was also marked as their choice by 0143, so it moves here too:
-- once 0143 has run the two cannot be told apart, and Other counts it the
-- way no category did.
UPDATE "agent_finance_transaction" AS "moved"
SET "spending_category_id" = "other_category"."id", "categorization_confidence" = NULL
FROM "agent_spending_category" AS "other_category"
WHERE "other_category"."agent_id" = "moved"."agent_id" AND "other_category"."is_other"
  AND "moved"."categorized_by" = 'person' AND "moved"."spending_category_id" IS NULL;
