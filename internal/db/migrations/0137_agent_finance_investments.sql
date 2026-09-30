-- What an investment account can hold, as a provider describes it: a
-- share, a fund, a bond, a coin, cash. One row per agent per provider's
-- security, kept when the finance source that brought it is deleted, so a
-- holding linked again finds the same security. Gone with the agent.
CREATE TABLE "agent_finance_security" (
    "id"                   character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"             character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "provider_kind"        text                     NOT NULL,
    "provider_security_id" text                     NOT NULL,

    "ticker_symbol"        text                     NOT NULL DEFAULT '',
    "security_name"        text                     NOT NULL,
    "security_kind"        character varying(20)    NOT NULL,
    "currency_code"        text                     NOT NULL DEFAULT '',

    -- The last close the provider reported, and its day.
    "close_price"          numeric(24,8),
    "close_price_on"       date,

    "provider_metadata"    jsonb                    NOT NULL DEFAULT '{}',

    "created_at"           timestamp with time zone NOT NULL,
    "modified_at"          timestamp with time zone NOT NULL,

    CHECK ("security_kind" IN ('cash', 'cryptocurrency', 'derivative', 'equity', 'etf', 'fixed_income', 'loan',
                               'mutual_fund', 'other'))
);
CREATE UNIQUE INDEX "agent_finance_security_provider" ON "agent_finance_security" ("agent_id", "provider_kind", "provider_security_id");

-- A buy, a sell, a cancelled trade, or a security moved into or out of an
-- investment account: cash swapped for a security or back inside the
-- account, so never spending or income, and not a finance transaction.
-- Gone with its finance account.
CREATE TABLE "agent_finance_trade" (
    "id"                  character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"            character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "finance_account_id"  character varying(32)    NOT NULL REFERENCES "agent_finance_account" ("id") ON DELETE CASCADE,
    "finance_security_id" character varying(32)    REFERENCES "agent_finance_security" ("id") ON DELETE SET NULL,
    "provider_trade_id"   text                     NOT NULL,

    "traded_on"           date                     NOT NULL,
    "trade_kind"          character varying(20)    NOT NULL,
    "trade_subkind"       text                     NOT NULL DEFAULT '',
    "traded_quantity"     numeric(24,8),
    "unit_price"          numeric(24,8),

    -- The cash the trade moved in the account, negative when cash left it
    -- (a buy), and what it cost in fees.
    "trade_amount"        numeric(19,4)            NOT NULL,
    "fee_amount"          numeric(19,4),
    "currency_code"       text                     NOT NULL,
    "description"         text                     NOT NULL DEFAULT '',

    "provider_metadata"   jsonb                    NOT NULL DEFAULT '{}',

    "created_at"          timestamp with time zone NOT NULL,
    "modified_at"         timestamp with time zone NOT NULL,

    CHECK ("trade_kind" IN ('buy', 'sell', 'cancel', 'transfer'))
);
CREATE UNIQUE INDEX "agent_finance_trade_provider" ON "agent_finance_trade" ("finance_account_id", "provider_trade_id");
CREATE INDEX "agent_finance_trade_traded" ON "agent_finance_trade" ("agent_id", "traded_on" DESC, "id" DESC);

-- A holding is an asset valued by a finance account for one security; the
-- account's own asset has no security. One of each per finance account.
ALTER TABLE "agent_asset"
    ADD COLUMN "finance_security_id" character varying(32) REFERENCES "agent_finance_security" ("id") ON DELETE SET NULL,
    DROP CONSTRAINT "agent_asset_finance_account_id_key";
CREATE UNIQUE INDEX "agent_asset_finance_account_security" ON "agent_asset" ("finance_account_id", "finance_security_id")
    NULLS NOT DISTINCT WHERE "finance_account_id" IS NOT NULL;

-- A holding's size and price on the day, and what was paid for it.
ALTER TABLE "agent_asset_valuation"
    ADD COLUMN "held_quantity" numeric(24,8),
    ADD COLUMN "unit_price"    numeric(24,8),
    ADD COLUMN "cost_basis"    numeric(19,4);
