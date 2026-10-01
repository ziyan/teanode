-- A mirrored copy: some providers report one charge, such as an
-- account-level fee, once on every account of a connection, each with its
-- own id. One copy is counted and the rest point at it and are left out of
-- every total, the way transfers are. What decided it is mirror detection,
-- or the person, whose "count this one" detection then leaves alone.
ALTER TABLE "agent_finance_transaction"
    ADD COLUMN "duplicate_of_transaction_id" character varying(32)
        REFERENCES "agent_finance_transaction" ("id") ON DELETE SET NULL,
    ADD COLUMN "duplicate_decided_by" character varying(40) NOT NULL DEFAULT '',
    ADD CONSTRAINT "agent_finance_transaction_duplicate_decided_by"
        CHECK ("duplicate_decided_by" IN ('', 'mirror_detection', 'person')),
    -- Only detection marks a copy; the person only says one counts. A
    -- counted copy that is deleted leaves its copies pointing nowhere and
    -- still marked by detection until it runs again, in the same sync.
    ADD CONSTRAINT "agent_finance_transaction_duplicate_of_detected"
        CHECK ("duplicate_of_transaction_id" IS NULL OR "duplicate_decided_by" = 'mirror_detection'),
    ADD CONSTRAINT "agent_finance_transaction_duplicate_of_other"
        CHECK ("duplicate_of_transaction_id" IS DISTINCT FROM "id");

-- Deleting a transaction finds its copies by this, and so does listing the
-- copies of a counted one.
CREATE INDEX "agent_finance_transaction_duplicate_of" ON "agent_finance_transaction" ("duplicate_of_transaction_id")
    WHERE "duplicate_of_transaction_id" IS NOT NULL;

-- The copies already stored, marked now rather than at each source's next
-- sync. The same rule as DetectMirroredFinanceTransactions: transactions of
-- one Plaid finance source on different accounts, every one of them an
-- investment account, with the same day, amount, currency and description
-- (trimmed, in any case), pending or posted. Other providers are
-- left alone: one SimpleFIN credential can reach several institutions,
-- and two deposit accounts of one Plaid item can each be charged the same
-- fee for real. Two on one account are never copies of each other: the
-- n-th of a day's repeats on one account goes with the n-th on each other
-- account, posted ones numbered first. The counted copy is a posted one
-- before a pending one, then the one stored first, then the one on the
-- oldest account.
WITH "scope" AS (
    SELECT "copy"."id", "copy"."finance_account_id", "account"."source_id", "copy"."is_pending", "copy"."posted_on", "copy"."amount",
        "copy"."currency_code", lower(btrim("copy"."description")) AS "description_key",
        "account"."account_kind" = 'investment' AS "is_investment_account",
        "copy"."created_at", "account"."created_at" AS "account_created_at"
    FROM "agent_finance_transaction" AS "copy"
    JOIN "agent_finance_account" AS "account" ON "account"."id" = "copy"."finance_account_id"
    JOIN "agent_source" AS "source" ON "source"."id" = "account"."source_id"
    WHERE btrim("copy"."description") <> ''
      AND COALESCE("source"."specification"->>'type', '') = 'plaid'
), "numbered" AS (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY "finance_account_id", "posted_on", "amount", "currency_code", "description_key"
        ORDER BY "is_pending", "created_at", "id") AS "occurrence"
    FROM "scope"
), "ranked" AS (
    SELECT "id",
        FIRST_VALUE("id") OVER "mirrored_set" AS "counted_id",
        COUNT(*) OVER "mirrored_set" AS "member_count",
        bool_and("is_investment_account") OVER "mirrored_set" AS "is_every_account_investment"
    FROM "numbered"
    WINDOW "mirrored_set" AS (PARTITION BY "source_id", "posted_on", "amount", "currency_code", "description_key", "occurrence"
        ORDER BY "is_pending", "created_at", "account_created_at", "finance_account_id", "id"
        ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING)
)
UPDATE "agent_finance_transaction" AS "target"
SET "duplicate_of_transaction_id" = "ranked"."counted_id", "duplicate_decided_by" = 'mirror_detection'
FROM "ranked"
WHERE "target"."id" = "ranked"."id" AND "ranked"."member_count" > 1 AND "ranked"."is_every_account_investment"
  AND "ranked"."counted_id" <> "ranked"."id";
