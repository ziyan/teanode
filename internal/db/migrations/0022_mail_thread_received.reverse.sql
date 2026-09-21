DROP INDEX IF EXISTS "mail_thread";
CREATE INDEX "mail_thread" ON "mail" ("thread_id");
