-- How precisely a fact's happened_at is known: 'day', 'month' or 'year', as
-- the date was given. A date given as "2023-01" was stored as the first of
-- the month, and every date was shown as a month, so two events a day apart
-- read as the same "Jan 2023" and the days between them could not be
-- counted. Empty where it was not recorded.
ALTER TABLE "agent_fact" ADD COLUMN "happened_precision" text NOT NULL DEFAULT '';
