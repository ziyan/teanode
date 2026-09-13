-- How far ahead an event's occurrences have been worked out.
--
-- The occurrence index reaches a horizon, and something has to extend it as
-- that horizon approaches or a standing meeting stops appearing. Finding the
-- events to extend by looking at their furthest occurrence does not work: a
-- series that has already finished has none in the window, so it matches for
-- ever, is rewritten every tick, and sits permanently in front of the events
-- that genuinely need extending.
--
-- This records the horizon each event was last worked out to, so each one is
-- extended once per advance and then left alone.
ALTER TABLE "calendar_object" ADD COLUMN "indexed_until" timestamptz;

-- What the worker claims: the ones whose horizon is closest to running out.
CREATE INDEX "calendar_object_indexed_until" ON "calendar_object" ("indexed_until")
    WHERE "recurring";
