-- Grouping a folder's messages into conversations reads mail.thread_id and
-- then wants the newest of each. The single-column index on thread_id finds
-- the rows; adding received_at lets PostgreSQL take them in the order the
-- grouping needs instead of sorting them afterwards.
DROP INDEX IF EXISTS "mail_thread";
CREATE INDEX "mail_thread" ON "mail" ("thread_id", "received_at" DESC);
