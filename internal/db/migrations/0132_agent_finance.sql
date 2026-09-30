-- The accounts and transactions a person's finance sources report: rows,
-- not documents, because the questions asked of them are sums over dates,
-- merchants and categories. Each keeps the provider's whole object as it
-- arrived (provider_metadata), since a provider sends more than these
-- columns hold and SimpleFIN keeps only about ninety days. agent_id is
-- repeated on both so every read filters on the owner directly. Gone with
-- the agent, and with the finance source that reported them.
CREATE TABLE "agent_finance_account" (
    "id"                  character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"            character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "source_id"           character varying(32)    NOT NULL REFERENCES "agent_source" ("id") ON DELETE CASCADE,

    -- The provider's id for the account, unique within its finance source.
    "provider_account_id" text                     NOT NULL,

    "account_name"        text                     NOT NULL DEFAULT '',
    "account_mask"        text                     NOT NULL DEFAULT '',
    "account_kind"        character varying(20)    NOT NULL,
    "currency_code"       text                     NOT NULL,

    -- As the provider reports them; empty when it does not say.
    "current_balance"     numeric(19,4),
    "available_balance"   numeric(19,4),
    "balance_at"          timestamp with time zone,

    "provider_metadata"   jsonb                    NOT NULL DEFAULT '{}',

    "created_at"          timestamp with time zone NOT NULL,
    "modified_at"         timestamp with time zone NOT NULL,

    CHECK ("account_kind" IN ('depository', 'credit', 'loan', 'investment', 'other'))
);
CREATE UNIQUE INDEX "agent_finance_account_provider" ON "agent_finance_account" ("source_id", "provider_account_id");
CREATE INDEX "agent_finance_account_agent" ON "agent_finance_account" ("agent_id");

CREATE TABLE "agent_finance_transaction" (
    "id"                              character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"                        character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "finance_account_id"              character varying(32)    NOT NULL REFERENCES "agent_finance_account" ("id") ON DELETE CASCADE,
    "provider_transaction_id"         text                     NOT NULL,

    "posted_on"                       date                     NOT NULL,
    "transacted_at"                   timestamp with time zone,

    -- Negative is money leaving the account, whichever way the provider
    -- signs it.
    "amount"                          numeric(19,4)            NOT NULL,
    "currency_code"                   text                     NOT NULL,

    "description"                     text                     NOT NULL DEFAULT '',
    "merchant_name"                   text                     NOT NULL DEFAULT '',
    "provider_category_primary"       text                     NOT NULL DEFAULT '',
    "provider_category_detailed"      text                     NOT NULL DEFAULT '',

    "is_pending"                      boolean                  NOT NULL,
    "pending_provider_transaction_id" text                     NOT NULL DEFAULT '',

    "provider_metadata"               jsonb                    NOT NULL DEFAULT '{}',

    "created_at"                      timestamp with time zone NOT NULL,
    "modified_at"                     timestamp with time zone NOT NULL
);
CREATE UNIQUE INDEX "agent_finance_transaction_provider" ON "agent_finance_transaction" ("finance_account_id", "provider_transaction_id");
CREATE INDEX "agent_finance_transaction_posted" ON "agent_finance_transaction" ("agent_id", "posted_on" DESC, "id" DESC);
