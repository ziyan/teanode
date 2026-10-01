-- Back to a transfer mark beside the spending category. What this loses:
-- a transfer's spending category from before 0143 was not kept, so a
-- transfer comes back marked and uncategorized; a person's "not a
-- transfer" that 0143 turned into their choice of spending category stays
-- that choice, without the mark; and the transfer category's name, if the
-- person renamed it, goes with it.
ALTER TABLE "agent_spending_rule"
    ADD COLUMN "is_transfer" boolean NOT NULL DEFAULT false,
    ALTER COLUMN "spending_category_id" DROP NOT NULL;
UPDATE "agent_spending_rule" AS "rule"
SET "is_transfer" = true, "spending_category_id" = NULL
FROM "agent_spending_category" AS "transfer_category"
WHERE "transfer_category"."id" = "rule"."spending_category_id" AND "transfer_category"."is_transfer";
ALTER TABLE "agent_spending_rule" ADD CONSTRAINT "agent_spending_rule_check" CHECK ("spending_category_id" IS NOT NULL OR "is_transfer");

ALTER TABLE "agent_finance_transaction"
    ADD COLUMN "is_transfer" boolean NOT NULL DEFAULT false,
    ADD COLUMN "transfer_marked_by" character varying(40) NOT NULL DEFAULT '';
UPDATE "agent_finance_transaction" AS "moved"
SET "is_transfer" = true,
    "transfer_marked_by" = CASE WHEN "moved"."categorized_by" = 'transfer_detection' THEN 'detection' ELSE "moved"."categorized_by" END,
    "spending_category_id" = NULL, "categorized_by" = '', "categorization_confidence" = NULL
FROM "agent_spending_category" AS "transfer_category"
WHERE "transfer_category"."id" = "moved"."spending_category_id" AND "transfer_category"."is_transfer";
UPDATE "agent_finance_transaction" SET "categorized_by" = '' WHERE "categorized_by" = 'transfer_detection';
ALTER TABLE "agent_finance_transaction"
    DROP CONSTRAINT "agent_finance_transaction_categorized_by",
    ADD CONSTRAINT "agent_finance_transaction_categorized_by"
        CHECK ("categorized_by" IN ('', 'person', 'spending_rule', 'provider_category_mapping', 'categorize_model')),
    ADD CONSTRAINT "agent_finance_transaction_transfer_marked_by"
        CHECK ("transfer_marked_by" IN ('', 'person', 'spending_rule', 'provider_category_mapping', 'detection')),
    ADD CONSTRAINT "agent_finance_transaction_transfer_origin"
        CHECK ("is_transfer" = ("transfer_marked_by" NOT IN ('', 'person')) OR "transfer_marked_by" = 'person');

DELETE FROM "agent_spending_category" WHERE "is_transfer";
DROP INDEX IF EXISTS "agent_spending_category_transfer";
ALTER TABLE "agent_spending_category"
    DROP CONSTRAINT IF EXISTS "agent_spending_category_transfer_not_income",
    DROP CONSTRAINT IF EXISTS "agent_spending_category_transfer_top_level",
    DROP COLUMN IF EXISTS "is_transfer";
