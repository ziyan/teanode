-- What one euro bought in each currency on each day, as the European
-- Central Bank published it. Server-wide: rates are public facts, not
-- anyone's data. A day with no row (a weekend, a TARGET holiday) takes the
-- latest earlier one.
CREATE TABLE "exchange_rate" (
    "rate_on"       date                     NOT NULL,
    "currency_code" text                     NOT NULL,
    "euro_rate"     numeric(20,10)           NOT NULL,
    "rate_source"   character varying(20)    NOT NULL,
    "created_at"    timestamp with time zone NOT NULL,
    PRIMARY KEY ("rate_on", "currency_code", "rate_source"),
    CHECK ("rate_source" IN ('ecb'))
);
CREATE INDEX "exchange_rate_latest" ON "exchange_rate" ("currency_code", "rate_source", "rate_on" DESC);

-- The one currency the person wants totals shown in. Empty means the
-- currency of their first finance account.
ALTER TABLE "agent" ADD COLUMN "reporting_currency_code" text NOT NULL DEFAULT '';
