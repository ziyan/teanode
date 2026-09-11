-- A person's budget may be set in money rather than tokens: what the day's
-- calls cost at the provider's own prices. Zero is no limit of its own and
-- falls back to the server's, as the token budget beside it does.
ALTER TABLE "agent" ADD COLUMN "daily_cost" double precision NOT NULL DEFAULT 0;
