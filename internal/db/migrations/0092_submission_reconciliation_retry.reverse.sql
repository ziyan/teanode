-- Accepted identities and pending work survive; only retry delays are lost.
DROP INDEX "mail_submission_reconcile_due";
ALTER TABLE "mail_submission" DROP COLUMN "reconcile_after";
CREATE INDEX "mail_submission_pending" ON "mail_submission" ("accepted_at", "owner_id", "submission_id") WHERE "reconciled_at" IS NULL;
