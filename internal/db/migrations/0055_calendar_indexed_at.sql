-- When an event's occurrences were last worked out.
--
-- The horizon alone was enough while every event could be indexed to it. One
-- that cannot -- a repeat fine enough to fill the cap on how many occurrences
-- one file may contribute -- reaches only as far as its last written
-- occurrence, which is inside the stretch the worker looks at. It is
-- therefore always running out, so it came back on every tick, and twenty of
-- them would hold up every other account's on a server where the question is
-- asked of the whole table.
--
-- Recording when the work was done lets such an event be left alone for a
-- while without writing down a horizon it never reached.
ALTER TABLE "calendar_object" ADD COLUMN "indexed_at" timestamptz;

-- The worker claims by horizon and skips what it has just done, so both
-- columns belong in the index it reads.
--
-- Ordered the way the worker orders: an event never worked out at all goes
-- first, which is the opposite of where a default index puts it. Written
-- without that the worker sorted every time, and the row that most needed
-- doing was the one the index could not offer first.
DROP INDEX "calendar_object_indexed_until";
CREATE INDEX "calendar_object_indexed_until" ON "calendar_object"
    ("indexed_until" ASC NULLS FIRST, "indexed_at")
    WHERE "recurring";
