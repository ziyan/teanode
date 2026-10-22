-- A finance account can be shared with a person or with a circle at a level.
-- finance_share_level is how much of it they see: balance (the account's name,
-- kind, balance and, for a card, its usage against its limit), spending
-- (balance, and monthly totals by spending category) or transactions
-- (spending, and each transaction's day, amount, merchant and category). A
-- finance account's receipts, annotations, the provider's own data, paystubs,
-- tax forms, sources, rules and budgets are never part of a share.
--
-- Only the person whose agent holds the account shares it. Sharing with a
-- person needs the sharer and that person to be in a circle together, and
-- sharing with a circle needs the sharer to be a member of it. That is checked
-- when the share is made. What keeps it true afterwards is not this table: a
-- circle's share reaches only members whose circle the account's owner is still
-- in, by the join on circle_member, and the code that takes a person out of a
-- circle deletes the direct shares between people who no longer share one.
CREATE TABLE "finance_account_share" (
    "id"                  character varying(32)  NOT NULL PRIMARY KEY,
    "finance_account_id"  character varying(32)  NOT NULL REFERENCES "agent_finance_account" ("id") ON DELETE CASCADE,
    "grantee_user_id"     character varying(32) REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_circle_id"   character varying(32) REFERENCES "circle" ("id") ON DELETE CASCADE,
    "finance_share_level" character varying(16)  NOT NULL
        CHECK ("finance_share_level" IN ('balance', 'spending', 'transactions')),
    "created_at"          timestamp with time zone NOT NULL,
    "created_by_user_id"  character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    CHECK (num_nonnulls("grantee_user_id", "grantee_circle_id") = 1),
    UNIQUE ("finance_account_id", "grantee_user_id"),
    UNIQUE ("finance_account_id", "grantee_circle_id")
);

-- "Which finance accounts are shared with me" starts from the grantee.
CREATE INDEX "finance_account_share_grantee_user" ON "finance_account_share" ("grantee_user_id") WHERE "grantee_user_id" IS NOT NULL;
CREATE INDEX "finance_account_share_grantee_circle" ON "finance_account_share" ("grantee_circle_id") WHERE "grantee_circle_id" IS NOT NULL;
