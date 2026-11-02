-- A person can share all of their finance with a person or with a circle at a
-- level, instead of one finance account at a time (finance_account_share).
-- Every finance account the owner holds now, and every one they link later, is
-- covered, so there is no column for an account.
--
-- finance_share_level is how much the grantee sees: spending (the owner's money
-- out and money in by month and spending category, across all their accounts,
-- with no account name, mask or balance) or transactions (spending, and each
-- transaction's day, amount, merchant and category, still with no account).
-- Balances stay with the shares of single accounts. Receipts, annotations, the
-- provider's own data, paystubs, tax forms, sources, rules and budgets are never
-- part of a share.
--
-- Only the owner shares. Sharing with a person needs the owner and that person
-- to be in a circle together, and sharing with a circle needs the owner to be a
-- member of it. That is checked when the share is made. What keeps it true
-- afterwards is not this table: a circle's share reaches only members whose
-- circle the owner is still in, by the join on circle_member, and the code that
-- takes a person out of a circle deletes the direct shares between people who no
-- longer share one.
CREATE TABLE "finance_share" (
    "id"                  character varying(32)    NOT NULL PRIMARY KEY,
    "owner_user_id"       character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_user_id"     character varying(32)    REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_circle_id"   character varying(32)    REFERENCES "circle" ("id") ON DELETE CASCADE,
    "finance_share_level" character varying(16)    NOT NULL
        CHECK ("finance_share_level" IN ('spending', 'transactions')),
    "created_at"          timestamp with time zone NOT NULL,
    "updated_at"          timestamp with time zone NOT NULL,
    "created_by_user_id"  character varying(32)    REFERENCES "user" ("id") ON DELETE SET NULL,
    CHECK (num_nonnulls("grantee_user_id", "grantee_circle_id") = 1),
    CHECK ("grantee_user_id" IS DISTINCT FROM "owner_user_id"),
    UNIQUE ("owner_user_id", "grantee_user_id"),
    UNIQUE ("owner_user_id", "grantee_circle_id")
);

-- "Whose finance is shared with me" starts from the grantee.
CREATE INDEX "finance_share_grantee_user" ON "finance_share" ("grantee_user_id") WHERE "grantee_user_id" IS NOT NULL;
CREATE INDEX "finance_share_grantee_circle" ON "finance_share" ("grantee_circle_id") WHERE "grantee_circle_id" IS NOT NULL;
