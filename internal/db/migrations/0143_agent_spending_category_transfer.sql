-- Transfer becomes a spending category. A finance transaction is a
-- transfer exactly when its spending category is the agent's transfer
-- category, so being a transfer and having a spending category can no
-- longer disagree, and a spending rule marks transfers the way it assigns
-- any spending category. Each agent has one, built in: it cannot be
-- deleted, be income or sit under a parent.
ALTER TABLE "agent_spending_category"
    ADD COLUMN "is_transfer" boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT "agent_spending_category_transfer_not_income" CHECK (NOT ("is_transfer" AND "is_income")),
    ADD CONSTRAINT "agent_spending_category_transfer_top_level" CHECK (NOT ("is_transfer" AND "parent_spending_category_id" IS NOT NULL));
CREATE UNIQUE INDEX "agent_spending_category_transfer" ON "agent_spending_category" ("agent_id") WHERE "is_transfer";

-- One for every agent, named "transfer" like the other built-in names, or
-- "transfer between own accounts" where the person already has a
-- "transfer" of their own, which stays theirs and keeps counting as it did.
INSERT INTO "agent_spending_category" ("id", "agent_id", "spending_category_name", "is_income", "is_hidden", "is_transfer", "created_at", "modified_at")
SELECT substr(md5("agent"."id" || '/transfer'), 1, 26), "agent"."id",
       CASE WHEN EXISTS (
           SELECT 1 FROM "agent_spending_category" AS "existing"
           WHERE "existing"."agent_id" = "agent"."id" AND lower("existing"."spending_category_name") = 'transfer'
       ) THEN 'transfer between own accounts' ELSE 'transfer' END,
       false, false, true, now(), now()
FROM "agent";

-- What paired a transfer is now what categorized it.
ALTER TABLE "agent_finance_transaction"
    DROP CONSTRAINT "agent_finance_transaction_categorized_by",
    ADD CONSTRAINT "agent_finance_transaction_categorized_by"
        CHECK ("categorized_by" IN ('', 'person', 'spending_rule', 'provider_category_mapping', 'categorize_model', 'transfer_detection'));

-- The person saying something is not a transfer becomes their choice of
-- the spending category it has (or of none), which pairing and the
-- provider category mapping leave alone as they left the unmarked
-- transfer alone.
UPDATE "agent_finance_transaction"
SET "categorized_by" = 'person', "categorization_confidence" = NULL
WHERE "transfer_marked_by" = 'person' AND NOT "is_transfer" AND "categorized_by" <> 'person';

-- Every transfer takes the transfer category, and what marked it becomes
-- what categorized it. The spending category it had beside the mark is
-- not kept: it counted for nothing while the mark stood.
UPDATE "agent_finance_transaction" AS "moved"
SET "spending_category_id" = "transfer_category"."id",
    "categorized_by" = CASE WHEN "moved"."transfer_marked_by" = 'detection' THEN 'transfer_detection' ELSE "moved"."transfer_marked_by" END,
    "categorization_confidence" = NULL
FROM "agent_spending_category" AS "transfer_category"
WHERE "transfer_category"."agent_id" = "moved"."agent_id" AND "transfer_category"."is_transfer" AND "moved"."is_transfer";

-- Dropping the columns drops the checks on them.
ALTER TABLE "agent_finance_transaction"
    DROP COLUMN "is_transfer",
    DROP COLUMN "transfer_marked_by";

-- A spending rule that marked transfers assigns the transfer category; one
-- that also assigned another spending category assigns the transfer
-- category alone, which is what its matches counted as. Every rule now
-- assigns a spending category.
UPDATE "agent_spending_rule" AS "rule"
SET "spending_category_id" = "transfer_category"."id"
FROM "agent_spending_category" AS "transfer_category"
WHERE "transfer_category"."agent_id" = "rule"."agent_id" AND "transfer_category"."is_transfer" AND "rule"."is_transfer";
ALTER TABLE "agent_spending_rule"
    DROP COLUMN "is_transfer",
    ALTER COLUMN "spending_category_id" SET NOT NULL;
