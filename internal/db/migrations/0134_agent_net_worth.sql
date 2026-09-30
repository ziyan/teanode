-- Everything that counts toward a person's net worth, what is owed
-- included (is_liability, set from the kind), and what each was worth on
-- each day. Net worth is summed from the history, never stored: a
-- correction for a past day then changes that day and the days after it.
-- Gone with the agent.
CREATE TABLE "agent_asset" (
    "id"                   character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"             character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "asset_name"           text                     NOT NULL,
    "asset_kind"           character varying(20)    NOT NULL,
    "is_liability"         boolean                  NOT NULL,
    "currency_code"        text                     NOT NULL,

    -- The finance account whose balance values it. Deleting the finance
    -- source keeps the asset and its history, turned manual.
    "finance_account_id"   character varying(32)    UNIQUE REFERENCES "agent_finance_account" ("id") ON DELETE SET NULL,

    -- Where its values normally come from.
    "valuation_source"     character varying(20)    NOT NULL,

    -- What the person allowed the agent to search the web with, and
    -- whether they allowed it at all.
    "estimate_description" text                     NOT NULL DEFAULT '',
    "is_estimate_allowed"  boolean                  NOT NULL DEFAULT false,

    -- Sold or paid off: it counts until this day and not after.
    "closed_on"            date,

    "created_at"           timestamp with time zone NOT NULL,
    "modified_at"          timestamp with time zone NOT NULL,

    CHECK ("asset_kind" IN ('cash', 'investment', 'retirement', 'property', 'vehicle', 'other_asset',
                            'credit_card', 'loan', 'mortgage', 'other_liability')),
    CHECK ("valuation_source" IN ('finance_sync', 'agent_reading', 'manual', 'agent_estimate'))
);
CREATE INDEX "agent_asset_agent" ON "agent_asset" ("agent_id");

-- One value of one asset on one day from one valuation source. Among one
-- day's rows manual wins, then finance_sync and agent_reading, then
-- agent_estimate; all are kept.
CREATE TABLE "agent_asset_valuation" (
    "id"               character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"         character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "asset_id"         character varying(32)    NOT NULL REFERENCES "agent_asset" ("id") ON DELETE CASCADE,
    "valued_on"        date                     NOT NULL,

    -- The size of the thing; is_liability on the asset gives the sign.
    -- Negative only for an account that is overdrawn.
    "value"            numeric(19,4)            NOT NULL,
    "currency_code"    text                     NOT NULL,
    "valuation_source" character varying(20)    NOT NULL,

    -- An estimate's range, what it rests on, and the pages it read.
    "estimate_low"     numeric(19,4),
    "estimate_high"    numeric(19,4),
    "valuation_note"   text                     NOT NULL DEFAULT '',
    "evidence_urls"    text[]                   NOT NULL DEFAULT '{}',

    "created_at"       timestamp with time zone NOT NULL,
    "modified_at"      timestamp with time zone NOT NULL,

    CHECK ("valuation_source" IN ('finance_sync', 'agent_reading', 'manual', 'agent_estimate'))
);
CREATE UNIQUE INDEX "agent_asset_valuation_day" ON "agent_asset_valuation" ("asset_id", "valued_on", "valuation_source");
CREATE INDEX "agent_asset_valuation_agent" ON "agent_asset_valuation" ("agent_id", "valued_on");
