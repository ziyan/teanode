-- What a provider billed for one model on one day (UTC), read from its bill by
-- the daily price check and kept beside the estimate worked out from token
-- counts. model is TeaNode's name for it, without a snapshot date, so it
-- matches the model in agent_usage. billed_amount is in currency_code and is
-- not rounded to cents. Rows older than 400 days are deleted by the check.
CREATE TABLE "agent_billed_day" (
    "provider_name" text NOT NULL,
    "billed_on" date NOT NULL,
    "model" text NOT NULL,
    "billed_amount" double precision NOT NULL,
    "currency_code" text NOT NULL,
    PRIMARY KEY ("provider_name", "billed_on", "model")
);
CREATE INDEX "agent_billed_day_billed_on" ON "agent_billed_day" ("billed_on");
