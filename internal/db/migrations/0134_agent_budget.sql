-- The person's own list of spending categories. The provider's category
-- is kept on each transaction as a hint; this is the one budgets count.
-- One level of parents. Gone with the agent.
CREATE TABLE "agent_spending_category" (
    "id"                          character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"                    character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "spending_category_name"      text                     NOT NULL,
    "parent_spending_category_id" character varying(32)    REFERENCES "agent_spending_category" ("id") ON DELETE SET NULL,
    "is_income"                   boolean                  NOT NULL DEFAULT false,
    "is_hidden"                   boolean                  NOT NULL DEFAULT false,
    "created_at"                  timestamp with time zone NOT NULL,
    "modified_at"                 timestamp with time zone NOT NULL,
    CHECK ("spending_category_name" <> '')
);
CREATE UNIQUE INDEX "agent_spending_category_name" ON "agent_spending_category" ("agent_id", "spending_category_name");

-- Each transaction's spending category, who gave it, and whether it is a
-- transfer between the person's own accounts. What the person chose
-- (categorized_by person, is_transfer_set_by_person) no sync, spending
-- rule or model overwrites.
ALTER TABLE "agent_finance_transaction"
    ADD COLUMN "spending_category_id" character varying(32) REFERENCES "agent_spending_category" ("id") ON DELETE SET NULL,
    ADD COLUMN "categorized_by" character varying(40) NOT NULL DEFAULT '',
    ADD COLUMN "categorization_confidence" numeric(5,4),
    ADD COLUMN "is_transfer" boolean NOT NULL DEFAULT false,
    ADD COLUMN "is_transfer_set_by_person" boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT "agent_finance_transaction_categorized_by"
        CHECK ("categorized_by" IN ('', 'person', 'spending_rule', 'provider_category_mapping', 'categorize_model'));
CREATE INDEX "agent_finance_transaction_uncategorized" ON "agent_finance_transaction" ("agent_id", "posted_on" DESC)
    WHERE "spending_category_id" IS NULL;

-- Assigns a spending category, or marks a transfer, to transactions that
-- match: match_text against the merchant (or the description when there
-- is none), case-insensitively, and optionally one account and amount
-- bounds. The first match by rule_priority wins.
CREATE TABLE "agent_spending_rule" (
    "id"                   character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"             character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "match_text"           text                     NOT NULL,
    "finance_account_id"   character varying(32)    REFERENCES "agent_finance_account" ("id") ON DELETE CASCADE,
    "minimum_amount"       numeric(19,4),
    "maximum_amount"       numeric(19,4),
    "spending_category_id" character varying(32)    REFERENCES "agent_spending_category" ("id") ON DELETE CASCADE,
    "is_transfer"          boolean                  NOT NULL DEFAULT false,
    "rule_priority"        integer                  NOT NULL,
    "created_at"           timestamp with time zone NOT NULL,
    "modified_at"          timestamp with time zone NOT NULL,
    CHECK ("match_text" <> ''),
    CHECK ("spending_category_id" IS NOT NULL OR "is_transfer")
);
CREATE INDEX "agent_spending_rule_priority" ON "agent_spending_rule" ("agent_id", "rule_priority");

-- A monthly amount for one spending category from a month on. A change is
-- a new row from a later month, so last March compares with last March's
-- budget; zero ends it.
CREATE TABLE "agent_budget" (
    "id"                   character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"             character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "spending_category_id" character varying(32)    NOT NULL REFERENCES "agent_spending_category" ("id") ON DELETE CASCADE,
    "monthly_amount"       numeric(19,4)            NOT NULL,
    "currency_code"        text                     NOT NULL,
    "effective_from"       date                     NOT NULL,
    "created_at"           timestamp with time zone NOT NULL,
    "modified_at"          timestamp with time zone NOT NULL,
    CHECK (EXTRACT(DAY FROM "effective_from") = 1),
    CHECK ("monthly_amount" >= 0)
);
CREATE UNIQUE INDEX "agent_budget_month" ON "agent_budget" ("spending_category_id", "effective_from");
CREATE INDEX "agent_budget_agent" ON "agent_budget" ("agent_id");

-- An amount to save by a date, measured by money not spent (cash_flow) or
-- by what the chosen assets are worth (asset_value).
CREATE TABLE "agent_savings_target" (
    "id"                  character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"            character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "savings_target_name" text                     NOT NULL,
    "target_amount"       numeric(19,4)            NOT NULL,
    "currency_code"       text                     NOT NULL,
    "target_on"           date                     NOT NULL,
    "target_measure"      character varying(20)    NOT NULL,
    "starting_amount"     numeric(19,4),
    "started_on"          date                     NOT NULL,
    "closed_on"           date,
    "created_at"          timestamp with time zone NOT NULL,
    "modified_at"         timestamp with time zone NOT NULL,
    CHECK ("target_measure" IN ('cash_flow', 'asset_value'))
);
CREATE INDEX "agent_savings_target_agent" ON "agent_savings_target" ("agent_id");

-- The assets an asset_value savings target measures.
CREATE TABLE "agent_savings_target_asset" (
    "savings_target_id" character varying(32) NOT NULL REFERENCES "agent_savings_target" ("id") ON DELETE CASCADE,
    "asset_id"          character varying(32) NOT NULL REFERENCES "agent_asset" ("id") ON DELETE CASCADE,
    PRIMARY KEY ("savings_target_id", "asset_id")
);

-- A budget or savings target crossing, written in code after a sync. Its
-- key (spending-category:<id>:2026-09:at_risk) becomes the alert's subject
-- key, so the same crossing is told once.
ALTER TABLE "agent_alert_candidate" ADD COLUMN "budget_key" text NOT NULL DEFAULT '';
CREATE INDEX "agent_alert_candidate_budget" ON "agent_alert_candidate" ("agent_id", "budget_key") WHERE "budget_key" <> '';
